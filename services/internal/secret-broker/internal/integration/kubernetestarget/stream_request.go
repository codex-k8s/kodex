package kubernetestarget

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// PrivilegedProtocol selects one explicit Kubernetes streaming protocol without fallback.
// Negotiation support is independently checked by its stream handler before reporting availability.
// PrivilegedProtocol выбирает один явный Kubernetes streaming protocol без fallback.
// Stream handler отдельно проверяет negotiation support до выдачи availability.
type PrivilegedProtocol = managedkubernetes.PrivilegedProtocol

const (
	// PrivilegedWebSocketV5 requires the exact v5.channel.k8s.io close-capable protocol.
	// PrivilegedWebSocketV5 требует exact v5.channel.k8s.io protocol с поддержкой close.
	PrivilegedWebSocketV5 = managedkubernetes.WebSocketV5
	// PrivilegedSPDYV4 requires the exact v4.channel.k8s.io protocol on a POST upgrade.
	// PrivilegedSPDYV4 требует exact v4.channel.k8s.io protocol при POST upgrade.
	PrivilegedSPDYV4 = managedkubernetes.SPDYV4
	// PrivilegedPortForwardWebSocketV4 selects native kubelet port-forward channel framing.
	// PrivilegedPortForwardWebSocketV4 выбирает native kubelet port-forward channel framing.
	PrivilegedPortForwardWebSocketV4 = managedkubernetes.PortForwardWebSocketV4
	// PrivilegedPortForwardSPDYV1 selects portforward.k8s.io with exact per-stream port headers.
	// PrivilegedPortForwardSPDYV1 выбирает portforward.k8s.io с exact per-stream port headers.
	PrivilegedPortForwardSPDYV1 = managedkubernetes.PortForwardSPDYV1
)

// PodStreamTarget carries only immutable owner-resolved target metadata, never authorization.
// PodUID is sent in the kubelet route; NodeUID must match the operator's serving-key binding.
// Container names identify the approved Pod container rather than a caller-selected host process.
// PodStreamTarget хранит только immutable owner-resolved target metadata, не authorization.
// PodUID передаётся в kubelet route; NodeUID обязан совпадать с operator serving-key binding.
// Container names задают approved Pod container, а не caller-selected host process.
type PodStreamTarget struct {
	Namespace, Name, UID, NodeName, NodeUID, Container string
}

// PodStreamOptions declares a bounded handler's channels and exact remote argv or Pod port.
// The broker never invokes a host shell; remote exec itself requires the separate per-effect gate.
// PodStreamOptions задаёт channels bounded handler и exact remote argv либо Pod port.
// Broker не запускает host shell; remote exec требует отдельный per-effect gate.
type PodStreamOptions struct {
	Command               []string
	Stdin, Stdout, Stderr bool
	TTY                   bool
	Ports                 []uint16
	Copy                  managedkubernetes.CopyProfile
}

// StreamRequest constructs only exec/attach/portForward routes with an explicit PodUID and returns
// all ordinary-Pod and node-proxy access requirements for that exact protocol. It accepts neither
// raw paths nor node-proxy URLs. Fresh owner admission, protected-Pod checks, protocol availability,
// frame bounds and transport cancel/join remain mandatory in the executing stream handler.
// StreamRequest создаёт только exec/attach/portForward routes с явным PodUID и возвращает полный
// ordinary-Pod/node-proxy access set для exact protocol. Raw paths и node-proxy URLs недопустимы.
// Исполняющий stream handler обязан проверить fresh owner admission, protected-Pod policy,
// protocol availability, frame bounds и transport cancel/join.
func (credential Credential) StreamRequest(ctx context.Context, pin PrivilegedTransportPin, now time.Time,
	operation string, target PodStreamTarget, options PodStreamOptions) (*http.Request, []AccessRequirement, error) {
	if ctx == nil || pin.ValidateFor(credential.target, now) != nil || target.NodeName != pin.NodeName ||
		target.NodeUID != pin.NodeUID || len(validation.IsDNS1123Subdomain(target.Name)) != 0 ||
		len(validation.IsDNS1123Label(target.Namespace)) != 0 || len(validation.IsDNS1123Label(target.Container)) != 0 {
		return nil, nil, errors.New(privilegedTransportUnavailable)
	}
	address := ResourceAddress{Version: "v1", Resource: "pods", Namespaced: true,
		Namespace: target.Namespace, Name: target.Name, UID: target.UID}
	if target.UID == "" || address.Validate() != nil {
		return nil, nil, errors.New(privilegedTransportUnavailable)
	}
	method, verb := http.MethodGet, "get"
	if pin.Protocol == PrivilegedSPDYV4 || pin.Protocol == PrivilegedPortForwardSPDYV1 {
		method, verb = http.MethodPost, "create"
	}
	pathOperation, subresource := "", ""
	switch operation {
	case "kubernetes.exec":
		pathOperation, subresource = "exec", "exec"
		if len(options.Command) == 0 || len(options.Command) > 64 || len(options.Ports) != 0 {
			return nil, nil, errors.New(privilegedTransportUnavailable)
		}
	case "kubernetes.cp.upload", "kubernetes.cp.download":
		expected, err := options.Copy.NativeCommand(operation)
		if err != nil || !slices.Equal(expected, options.Command) || len(options.Ports) != 0 || options.TTY || !options.Stdout || !options.Stderr || options.Stdin != (operation == "kubernetes.cp.upload") {
			return nil, nil, errors.New(privilegedTransportUnavailable)
		}
		pathOperation, subresource = "exec", "exec"
	case "kubernetes.attach":
		pathOperation, subresource = "attach", "attach"
		if len(options.Command) != 0 || len(options.Ports) != 0 {
			return nil, nil, errors.New(privilegedTransportUnavailable)
		}
	case "kubernetes.port-forward":
		pathOperation, subresource = "portForward", "portforward"
		if len(options.Command) != 0 || len(options.Ports) == 0 || len(options.Ports) > managedkubernetes.MaximumForwardPorts || options.Stdin || options.Stdout || options.Stderr || options.TTY {
			return nil, nil, errors.New(privilegedTransportUnavailable)
		}
	default:
		return nil, nil, errors.New(privilegedTransportUnavailable)
	}
	if subresource == "portforward" {
		if pin.Protocol != PrivilegedPortForwardWebSocketV4 && pin.Protocol != PrivilegedPortForwardSPDYV1 {
			return nil, nil, errors.New(privilegedTransportUnavailable)
		}
	} else if pin.Protocol != PrivilegedWebSocketV5 && pin.Protocol != PrivilegedSPDYV4 {
		return nil, nil, errors.New(privilegedTransportUnavailable)
	}
	query := url.Values{}
	if subresource != "portforward" {
		if (!options.Stdin && !options.Stdout && !options.Stderr) || (options.TTY && options.Stderr) {
			return nil, nil, errors.New(privilegedTransportUnavailable)
		}
		query.Set(corev1.ExecStdinParam, strconv.FormatBool(options.Stdin))
		query.Set(corev1.ExecStdoutParam, strconv.FormatBool(options.Stdout))
		query.Set(corev1.ExecStderrParam, strconv.FormatBool(options.Stderr))
		query.Set(corev1.ExecTTYParam, strconv.FormatBool(options.TTY))
		total := 0
		for _, argument := range options.Command {
			total += len(argument)
			if len(argument) > 4096 || !utf8.ValidString(argument) || strings.ContainsRune(argument, 0) || total > 16<<10 {
				return nil, nil, errors.New(privilegedTransportUnavailable)
			}
			query.Add(corev1.ExecCommandParam, argument)
		}
		if len(options.Command) > 0 && options.Command[0] == "" {
			return nil, nil, errors.New(privilegedTransportUnavailable)
		}
	} else {
		for _, port := range options.Ports {
			if port == 0 {
				return nil, nil, errors.New(privilegedTransportUnavailable)
			}
			query.Add(corev1.PortHeader, strconv.Itoa(int(port)))
		}
	}
	endpoint, err := url.Parse(pin.ServerURL)
	if err != nil {
		return nil, nil, errors.New(privilegedTransportUnavailable)
	}
	parts := []string{pathOperation, target.Namespace, target.Name, target.UID}
	if subresource != "portforward" {
		parts = append(parts, target.Container)
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery = "/"+strings.Join(parts, "/"), "", query.Encode()
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), nil)
	if err != nil {
		return nil, nil, errors.New(privilegedTransportUnavailable)
	}
	checks := []AccessRequirement{
		{Version: "v1", Resource: "pods", Namespace: target.Namespace, Name: target.Name, Verb: "get", Namespaced: true},
		{Version: "v1", Resource: "pods", Namespace: target.Namespace, Name: target.Name, Subresource: subresource, Verb: verb, Namespaced: true},
		{Version: "v1", Resource: "nodes", Name: target.NodeName, Subresource: "proxy", Verb: verb},
	}
	for _, check := range checks {
		if check.Validate() != nil {
			return nil, nil, errors.New(privilegedTransportUnavailable)
		}
	}
	return request, checks, nil
}
