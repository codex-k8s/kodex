package kubernetestarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// PodStreamPreparer loads only exact purpose credentials and never starts a process during discovery.
// PodStreamPreparer читает только exact purpose credentials и не запускает процесс при discovery.
type PodStreamPreparer struct{ identity *ResourcePreparer }

// NewPodStreamPreparer shares the private identity namespace guard with the resource consumer.
// NewPodStreamPreparer использует ту же проверку private identity namespace, что и resource consumer.
func NewPodStreamPreparer(reader targetrepo.IdentityMaterialReader, namespace string) (*PodStreamPreparer, error) {
	identity, err := NewResourcePreparer(reader, namespace)
	if err != nil {
		return nil, err
	}
	return &PodStreamPreparer{identity: identity}, nil
}

// ObserveKubernetesPod resolves private material once for bounded native discovery, without grants or effects.
// ObserveKubernetesPod однократно разрешает private material для bounded native discovery без grants/effects.
func (factory *PodStreamPreparer) ObserveKubernetesPod(ctx context.Context, snapshot value.KubernetesOperationSnapshot) (managedkubernetes.ChildCandidate, error) {
	if factory == nil || factory.identity == nil || ctx == nil || ctx.Err() != nil ||
		(!watchOperation(snapshot.Operation) && snapshot.Operation != "kubernetes.logs" && snapshot.Operation != "kubernetes.exec" && snapshot.Operation != "kubernetes.attach" && snapshot.Operation != "kubernetes.port-forward" && snapshot.Operation != "kubernetes.cp.upload" && snapshot.Operation != "kubernetes.cp.download") ||
		!managedkubernetes.ValidDigest(snapshot.Digest) || snapshot.IdentityGeneration < 1 ||
		snapshot.Identity.Validate(factory.identity.namespace) != nil || snapshot.IdentityRef != snapshot.Identity.IdentityRef ||
		snapshot.Identity.TargetRef != snapshot.Target.ID || !snapshot.ExpiresAt.After(time.Now()) || !managedkubernetes.ValidDigest(snapshot.SubjectDigest) {
		return managedkubernetes.ChildCandidate{}, targetrepo.ErrOperationUnavailable
	}
	input, canonical, err := managedkubernetes.DecodeOperationInput(snapshot.Operation, snapshot.NormalizedInput)
	defer clear(canonical)
	if err != nil || !bytes.Equal(canonical, snapshot.NormalizedInput) {
		return managedkubernetes.ChildCandidate{}, targetrepo.ErrOperationUnavailable
	}
	check, cancel := context.WithDeadline(ctx, snapshot.ExpiresAt)
	defer cancel()
	read, finish := context.WithTimeout(check, 5*time.Second)
	credential, err := LoadCredential(read, factory.identity.reader, snapshot.Identity, snapshot.Target, time.Now())
	readErr := read.Err()
	finish()
	if err != nil || readErr != nil {
		return managedkubernetes.ChildCandidate{}, targetrepo.ErrOperationUnavailable
	}
	if watchOperation(snapshot.Operation) {
		candidate, _, err := credential.observeWatch(check, snapshot.Operation, input, snapshot.SubjectDigest, snapshot.ProtectedNamespaces)
		return candidate, err
	}
	return credential.ObservePodStream(check, snapshot.Operation, input, snapshot.SubjectDigest, snapshot.ProtectedNamespaces)
}

// preparedPodStream retains immutable metadata and a single-attempt guard, never cached credentials.
// preparedPodStream хранит immutable metadata и single-attempt guard без кеширования credentials.
type preparedPodStream struct {
	mutex            sync.Mutex
	closed, consumed bool
	cancel           context.CancelFunc
	done             chan struct{}
	binding          targetrepo.PodStreamPreparationBinding
	current          value.KubernetesOperationSnapshot
	access           *BoundPodStreamAccess
	logsAccess       *boundLogsAccess
	watchAccess      *boundWatchAccess
	options          PodStreamOptions
}

// String hides command input and private identity locators from diagnostic formatting.
// String скрывает command input и private identity locators при диагностическом форматировании.
func (*preparedPodStream) String() string { return "[managed Kubernetes prepared Pod stream]" }

// GoString preserves redaction for detailed formatting.
// GoString сохраняет redaction для подробного форматирования.
func (prepared *preparedPodStream) GoString() string { return prepared.String() }

// PrepareKubernetesPodStream checks the final immutable child against a fresh observation before Begin.
// Only qualified exec/attach/copy transports and the explicit SPDY connection-lifecycle PF profile are opened; PF WSv4 fails before admission.
// PrepareKubernetesPodStream сверяет final immutable child со свежим наблюдением до Begin.
// Factory открывает квалифицированные exec/attach/copy transports и explicit SPDY connection-lifecycle PF profile; PF WSv4 отклоняется до admission.
func (factory *PodStreamPreparer) PrepareKubernetesPodStream(ctx context.Context, snapshot value.KubernetesOperationSnapshot, child targetrepo.OperationChild) (targetrepo.PodStreamPreparation, error) {
	if factory == nil || factory.identity == nil || ctx == nil || ctx.Err() != nil || !managedkubernetes.ValidDigest(snapshot.Digest) ||
		!managedkubernetes.ValidLocator(child.Ref) || (child.State != "READY" && child.State != "WAITING_OWNER") ||
		child.Operation != snapshot.Operation ||
		!bytes.Equal(child.NormalizedInput, snapshot.NormalizedInput) || child.Observation == nil || child.Observation.Validate() != nil || child.Resource != child.Observation.Resource {
		return nil, targetrepo.ErrOperationUnavailable
	}
	sum := sha256.Sum256(child.NormalizedInput)
	if child.InputDigest != hex.EncodeToString(sum[:]) {
		return nil, targetrepo.ErrOperationUnavailable
	}
	owned, canonical, err := managedkubernetes.DecodeOperationInput(child.Operation, child.NormalizedInput)
	defer clear(canonical)
	if err != nil || !bytes.Equal(canonical, child.NormalizedInput) {
		return nil, targetrepo.ErrOperationUnavailable
	}
	if watchOperation(child.Operation) {
		return factory.prepareWatchStream(ctx, snapshot, child, owned)
	}
	if child.Operation == "kubernetes.logs" {
		return factory.prepareLogsStream(ctx, snapshot, child, owned)
	}
	if child.Observation.PodWitness == nil || child.NodeTransport == nil || !(validNativeWebSocketOperation(child.Operation, child.NodeTransport.Protocol) || validNativeSPDYOperation(child.Operation, child.NodeTransport.Protocol)) {
		return nil, targetrepo.ErrOperationUnavailable
	}
	if child.Operation == "kubernetes.port-forward" && child.NodeTransport.Protocol != PrivilegedPortForwardSPDYV1 {
		return nil, targetrepo.ErrPortForwardProfileUnsupported
	}
	options, duration, err := podStreamInputOptions(child.Operation, owned)
	if err != nil {
		return nil, err
	}
	witness := *child.Observation.PodWitness
	if witness.NodeName != child.NodeTransport.NodeName || witness.NodeUID != child.NodeTransport.NodeUID {
		return nil, targetrepo.ErrOperationUnavailable
	}
	observed, err := factory.ObserveKubernetesPod(ctx, snapshot)
	if err != nil || observed.PodWitness == nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	observed.PodWitness.NodeUID = witness.NodeUID
	if observed.NamespaceUID == "" {
		observed.NamespaceUID = child.Observation.NamespaceUID
	}
	if !reflect.DeepEqual(observed, *child.Observation) {
		return nil, targetrepo.ErrOperationUnavailable
	}
	current, err := ownedPodStreamAccessSnapshot(snapshot, *child.NodeTransport)
	if err != nil {
		return nil, err
	}
	pod := PodStreamTarget{Namespace: child.Resource.Namespace, Name: child.Resource.Name, UID: child.Resource.UID, NodeName: witness.NodeName, NodeUID: witness.NodeUID, Container: witness.Container}
	access, err := NewBoundPodStreamAccess(factory.identity.reader, current, child.Operation, pod, options)
	if err != nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	deadline := time.Now().Add(duration)
	if current.NodeTransport.ExpiresAt.Before(deadline) {
		deadline = current.NodeTransport.ExpiresAt
	}
	if !deadline.After(time.Now()) {
		return nil, targetrepo.ErrOperationUnavailable
	}
	return &preparedPodStream{current: current, access: access, options: options,
		binding: targetrepo.PodStreamPreparationBinding{SnapshotDigest: snapshot.Digest, ChildRef: child.Ref, Operation: child.Operation, InputDigest: child.InputDigest,
			TargetDigest: snapshot.Target.MetadataDigest(), SubjectDigest: snapshot.SubjectDigest, Observation: observed, HardDeadline: deadline}}, nil
}

// podStreamInputOptions converts only schema-validated options to the existing native channel contract.
// podStreamInputOptions преобразует только schema-validated options в existing native channel contract.
func podStreamInputOptions(operation string, input managedkubernetes.OperationInput) (PodStreamOptions, time.Duration, error) {
	values, err := resourceOperationOptions(input)
	if err != nil {
		return PodStreamOptions{}, 0, targetrepo.ErrOperationUnavailable
	}
	seconds, valid := resourceInteger(values["timeoutSeconds"])
	if !valid || seconds < 1 || seconds > 300 {
		return PodStreamOptions{}, 0, targetrepo.ErrOperationUnavailable
	}
	if operation == "kubernetes.cp.upload" || operation == "kubernetes.cp.download" {
		profile, err := managedkubernetes.DecodeCopyProfile(operation, input)
		if err != nil {
			return PodStreamOptions{}, 0, targetrepo.ErrOperationUnavailable
		}
		command, err := profile.NativeCommand(operation)
		if err != nil {
			return PodStreamOptions{}, 0, targetrepo.ErrOperationUnavailable
		}
		return PodStreamOptions{Copy: profile, Command: command, Stdin: operation == "kubernetes.cp.upload", Stdout: true, Stderr: true}, time.Duration(seconds) * time.Second, nil
	}
	if operation == "kubernetes.port-forward" {
		profile, err := managedkubernetes.DecodePortForwardProfile(input)
		if err != nil {
			return PodStreamOptions{}, 0, targetrepo.ErrOperationUnavailable
		}
		ports := make([]uint16, len(profile.Ports))
		for index, port := range profile.Ports {
			ports[index] = port.TargetPort
		}
		return PodStreamOptions{Ports: ports}, time.Duration(seconds) * time.Second, nil
	}
	if operation != "kubernetes.exec" && operation != "kubernetes.attach" {
		return PodStreamOptions{}, 0, targetrepo.ErrOperationUnavailable
	}
	options := PodStreamOptions{Stdout: true}
	options.Stdin, _ = values["stdin"].(bool)
	options.TTY, _ = values["tty"].(bool)
	options.Stderr = !options.TTY
	if command, present := values["command"].([]any); present {
		for _, argument := range command {
			value, ok := argument.(string)
			if !ok {
				return PodStreamOptions{}, 0, targetrepo.ErrOperationUnavailable
			}
			options.Command = append(options.Command, value)
		}
	}
	return options, time.Duration(seconds) * time.Second, nil
}

// ownedPodStreamAccessSnapshot copies only metadata needed by fresh access checks, retaining private locator values.
// JSON is used only for the public target structure; secret-free locator fields are copied explicitly.
// ownedPodStreamAccessSnapshot копирует лишь metadata свежих access checks, сохраняя private locator values.
// JSON используется только для public target structure; поля locator копируются явно.
func ownedPodStreamAccessSnapshot(snapshot value.KubernetesOperationSnapshot, node value.KubernetesNodeTransport) (value.KubernetesOperationSnapshot, error) {
	raw, err := json.Marshal(snapshot.Target)
	if err != nil {
		return value.KubernetesOperationSnapshot{}, targetrepo.ErrOperationUnavailable
	}
	defer clear(raw)
	var target value.KubernetesTarget
	if json.Unmarshal(raw, &target) != nil {
		return value.KubernetesOperationSnapshot{}, targetrepo.ErrOperationUnavailable
	}
	node.CertificateAuthorityPEM = bytes.Clone(node.CertificateAuthorityPEM)
	node.AddressIPs = slices.Clone(node.AddressIPs)
	return value.KubernetesOperationSnapshot{Identity: snapshot.Identity, IdentityRef: snapshot.IdentityRef, SubjectDigest: snapshot.SubjectDigest,
		Target: target, NodeTransport: &node, ExpiresAt: snapshot.ExpiresAt, ProtectedNamespaces: slices.Clone(snapshot.ProtectedNamespaces)}, nil
}

// Binding returns an owned observation copy without sharing its mutable witness pointer.
// Binding возвращает собственную копию observation без общего mutable witness pointer.
func (prepared *preparedPodStream) Binding() (targetrepo.PodStreamPreparationBinding, bool) {
	if prepared == nil {
		return targetrepo.PodStreamPreparationBinding{}, false
	}
	prepared.mutex.Lock()
	defer prepared.mutex.Unlock()
	if prepared.closed || prepared.consumed {
		return targetrepo.PodStreamPreparationBinding{}, false
	}
	result := prepared.binding
	if result.Observation.PodWitness != nil {
		witness := *result.Observation.PodWitness
		result.Observation.PodWitness = &witness
	}
	return result, true
}

// TargetAccess returns the independent metadata verifier; it cannot open or retry the transport.
// TargetAccess возвращает независимый metadata verifier, который не открывает и не повторяет transport.
func (prepared *preparedPodStream) TargetAccess() targetrepo.StreamTargetAccess {
	if prepared == nil {
		return nil
	}
	if prepared.watchAccess != nil {
		return prepared.watchAccess
	}
	if prepared.logsAccess != nil {
		return prepared.logsAccess
	}
	return prepared.access
}

// Open consumes the preparation before credential I/O and transfers a single transport after exact admission.
// Any lost reply remains an uncertain attempted effect; a second Open is refused.
// Open расходует preparation до credential I/O и передаёт один transport после exact admission.
// Lost reply остаётся uncertain attempted effect; второй Open отклоняется.
func (prepared *preparedPodStream) Open(lifecycle, attempt context.Context, binding managedkubernetes.Binding, admission targetrepo.EffectAdmission) (targetrepo.OpenedPodStream, managedkubernetes.HandshakeReceipt, error) {
	var empty managedkubernetes.HandshakeReceipt
	if prepared == nil || lifecycle == nil || attempt == nil || lifecycle.Err() != nil || attempt.Err() != nil {
		return nil, empty, targetrepo.ErrOperationUnavailable
	}
	prepared.mutex.Lock()
	if prepared.closed || prepared.consumed {
		prepared.mutex.Unlock()
		return nil, empty, targetrepo.ErrOperationUnavailable
	}
	prepared.consumed = true
	check, cancel := context.WithCancel(attempt)
	prepared.cancel, prepared.done = cancel, make(chan struct{})
	prepared.mutex.Unlock()
	defer func() { cancel(); close(prepared.done) }()
	if admission.Stream != nil {
		grant := *admission.Stream
		grant.InputChannels = slices.Clone(grant.InputChannels)
		grant.OutputChannels = slices.Clone(grant.OutputChannels)
		admission.Stream = &grant
	}
	bindingDigest, err := managedkubernetes.BindingDigest(binding)
	if err != nil || admission.GateRef != "" || admission.ReconcileOnly || admission.AcceptedResult != nil || admission.ChildInputDigest != prepared.binding.InputDigest ||
		!managedkubernetes.ValidLocator(admission.EffectRef) || admission.EffectGeneration < 1 || admission.Stream == nil || !admission.Stream.Valid(time.Now()) || admission.Stream.Generation != binding.Generation ||
		!managedkubernetes.ValidDigest(prepared.binding.SnapshotDigest) || !prepared.grantChannels(*admission.Stream) ||
		!admission.ExpiresAt.After(time.Now()) || admission.ExpiresAt.After(prepared.current.ExpiresAt) || admission.Stream.ExpiresAt.After(prepared.binding.HardDeadline) {
		return nil, empty, targetrepo.ErrOperationUnavailable
	}
	creditContext, finish := context.WithDeadline(check, admission.ExpiresAt)
	defer finish()
	var credential Credential
	var credit time.Time
	if prepared.watchAccess != nil {
		credential, credit, err = prepared.watchAccess.authorizedCredential(creditContext, prepared.current)
	} else if prepared.logsAccess != nil {
		credential, credit, err = prepared.logsAccess.authorizedCredential(creditContext, prepared.current)
	} else {
		credential, credit, err = prepared.access.authorizedCredential(creditContext, prepared.current)
	}
	if err != nil {
		return nil, empty, targetrepo.ErrOperationUnavailable
	}
	if admission.ExpiresAt.Before(credit) {
		credit = admission.ExpiresAt
	}
	origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: prepared.binding.SnapshotDigest, BindingDigest: bindingDigest, ChildInputDigest: prepared.binding.InputDigest,
		EffectRef: admission.EffectRef, EffectGeneration: admission.EffectGeneration, StreamRef: admission.Stream.Ref, StreamGeneration: admission.Stream.Generation,
		Operation: prepared.binding.Operation, TargetRef: prepared.current.Target.ID, TargetRevision: int64(prepared.current.Target.Revision)}
	if origin.Validate() != nil {
		return nil, empty, targetrepo.ErrOperationUnavailable
	}
	var stream targetrepo.OpenedPodStream
	var receipt managedkubernetes.HandshakeReceipt
	if prepared.watchAccess != nil {
		a := prepared.watchAccess
		var native *nativeWatchHTTP
		native, err = credential.openWatchHTTP(lifecycle, creditContext, a.operation, a.input, a.observation.Resource, a.schema, a.protected, admission.Stream.ExpiresAt, credit, admission.Stream.MaximumTotalBytes)
		if err == nil {
			stream = native
			receipt, err = native.HandshakeReceipt(origin)
		}
	} else if prepared.logsAccess != nil {
		var native *nativeReadHTTP
		a := prepared.logsAccess
		native, err = credential.openLogsHTTP(lifecycle, creditContext, a.input, a.address, a.pod.Container, admission.Stream.ExpiresAt, credit, admission.Stream.MaximumTotalBytes)
		if err == nil {
			stream = native
			receipt, err = native.HandshakeReceipt(origin)
		}
	} else if prepared.access.pin.Protocol == PrivilegedSPDYV4 {
		var native *nativeExecSPDY
		native, err = credential.openExecSPDY(lifecycle, creditContext, prepared.access.pin, time.Now(), prepared.binding.Operation, prepared.access.pod, prepared.options, admission.Stream.ExpiresAt, credit, admission.Stream.MaximumTotalBytes)
		if err == nil {
			stream = native
			receipt, err = native.HandshakeReceipt(origin)
		}
	} else if prepared.access.pin.Protocol == PrivilegedPortForwardSPDYV1 {
		var native *nativeForwardConnections
		native, err = credential.openPortForwardConnectionsSPDY(lifecycle, creditContext, prepared.access.pin, time.Now(), prepared.access.pod, prepared.options.Ports, admission.Stream.ExpiresAt, credit, admission.Stream.MaximumTotalBytes)
		if err == nil {
			stream = native
			receipt, err = native.HandshakeReceipt(origin)
		}
	} else if prepared.binding.Operation == "kubernetes.port-forward" {
		return nil, empty, targetrepo.ErrPortForwardProfileUnsupported
	} else {
		var native *nativeExecWebSocket
		native, err = credential.openExecWebSocket(lifecycle, creditContext, prepared.access.pin, time.Now(), prepared.binding.Operation, prepared.access.pod, prepared.options, admission.Stream.ExpiresAt, credit, admission.Stream.MaximumTotalBytes)
		if err == nil {
			stream = native
			receipt, err = native.HandshakeReceipt(origin)
		}
	}
	if stream == nil {
		return nil, empty, targetrepo.ErrOperationUnavailable
	}
	prepared.mutex.Lock()
	refused := prepared.closed || check.Err() != nil || lifecycle.Err() != nil || err != nil
	prepared.mutex.Unlock()
	if refused {
		_ = stream.Close()
		return nil, empty, targetrepo.ErrOperationUnavailable
	}
	return stream, receipt, nil
}

// grantChannels refuses owner metadata inconsistent with the exact prepared input channels.
// Output channels retain the owner's fixed exec family; TTY additionally excludes stderr on the wire.
// grantChannels отклоняет owner metadata, не соответствующие exact prepared input channels.
// Output channels сохраняют фиксированную exec family owner; TTY дополнительно исключает stderr на wire.
func (prepared *preparedPodStream) grantChannels(grant value.KubernetesStreamGrant) bool {
	if prepared.binding.Operation == "kubernetes.logs" || watchOperation(prepared.binding.Operation) {
		return len(grant.InputChannels) == 0 && slices.Equal([]string{"STDOUT"}, grant.OutputChannels)
	}
	if prepared.binding.Operation == "kubernetes.port-forward" {
		return slices.Equal([]string{"PORT_FORWARD"}, grant.InputChannels) && slices.Equal([]string{"PORT_FORWARD", "CONTROL"}, grant.OutputChannels)
	}
	if prepared.binding.Operation == "kubernetes.cp.upload" {
		return slices.Equal([]string{"FILE_CONTENT"}, grant.InputChannels) && slices.Equal([]string{"STDOUT", "STDERR", "CONTROL"}, grant.OutputChannels)
	}
	if prepared.binding.Operation == "kubernetes.cp.download" {
		return len(grant.InputChannels) == 0 && slices.Equal([]string{"FILE_CONTENT", "STDERR", "CONTROL"}, grant.OutputChannels)
	}
	inputs := []string{}
	if prepared.options.Stdin {
		inputs = append(inputs, "STDIN")
	}
	if prepared.options.TTY {
		inputs = append(inputs, "TERMINAL_RESIZE")
	}
	return slices.Equal(inputs, grant.InputChannels) && slices.Equal([]string{"STDOUT", "STDERR", "CONTROL"}, grant.OutputChannels)
}

// Close waits for any in-progress open; transferred streams remain the caller's explicit responsibility.
// Close ожидает in-progress open; переданные streams остаются явной ответственностью caller.
func (prepared *preparedPodStream) Close() {
	if prepared == nil {
		return
	}
	prepared.mutex.Lock()
	prepared.closed = true
	if prepared.cancel != nil {
		prepared.cancel()
	}
	done := prepared.done
	prepared.mutex.Unlock()
	if done != nil {
		<-done
	}
}

// The assertion keeps the private factory aligned with the credential-free domain port.
// Проверка сохраняет соответствие private factory и credential-free domain port.
var _ targetrepo.PodStreamPreparer = (*PodStreamPreparer)(nil)
