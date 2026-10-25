package kubernetesadmission

import (
	"errors"
	"reflect"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// expectedResources supplies independent literal spec expectations, not objects read by Check.
// expectedResources задаёт независимые literal spec expectations, а не прочитанные Check объекты.
func expectedResources() (map[string]any, map[string]any) {
	return map[string]any{"spec": map[string]any{"failurePolicy": "Fail", "validations": []any{map[string]any{"expression": "true"}}}},
		map[string]any{"spec": map[string]any{"policyName": "fixture", "validationActions": []any{"Deny"}}}
}

// admissionFixture constructs separate synthetic served objects and fixed caller error identities.
// It contacts no cluster and obtains no kubeconfig, credentials or expected data from the checker.
// admissionFixture создаёт отдельные синтетические served objects и fixed errors вызывающего.
// К кластеру не обращается; kubeconfig, credentials и expected data из checker не получает.
func admissionFixture() (*admissionv1.ValidatingAdmissionPolicy, *admissionv1.ValidatingAdmissionPolicyBinding, Failures) {
	fail := admissionv1.Fail
	policy := &admissionv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "fixture", UID: "synthetic-policy", ResourceVersion: "7", Generation: 3},
		Spec:       admissionv1.ValidatingAdmissionPolicySpec{FailurePolicy: &fail, Validations: []admissionv1.Validation{{Expression: "true"}}},
		Status:     admissionv1.ValidatingAdmissionPolicyStatus{ObservedGeneration: 3, TypeChecking: &admissionv1.TypeChecking{}},
	}
	binding := &admissionv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "fixture", UID: "synthetic-binding", ResourceVersion: "9"},
		Spec:       admissionv1.ValidatingAdmissionPolicyBindingSpec{PolicyName: "fixture", ValidationActions: []admissionv1.ValidationAction{admissionv1.Deny}},
	}
	return policy, binding, Failures{Invalid: errors.New("synthetic source rejection"), Unavailable: errors.New("synthetic dependency rejection"), Conflict: errors.New("synthetic observed-state rejection")}
}

// TestCheckObservedState rejects every required identity/status/spec failure independently while
// keeping policy/binding reads exact and read-only. Expected specs are separate literal fixtures.
// TestCheckObservedState отдельно отклоняет каждую обязательную ошибку identity/status/spec,
// сохраняя exact read-only чтения policy/binding. Expected specs заданы отдельными literals.
func TestCheckObservedState(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*admissionv1.ValidatingAdmissionPolicy, *admissionv1.ValidatingAdmissionPolicyBinding)
	}{
		{"accepted", func(*admissionv1.ValidatingAdmissionPolicy, *admissionv1.ValidatingAdmissionPolicyBinding) {}},
		{"policy_uid", func(p *admissionv1.ValidatingAdmissionPolicy, _ *admissionv1.ValidatingAdmissionPolicyBinding) {
			p.UID = ""
		}},
		{"policy_version", func(p *admissionv1.ValidatingAdmissionPolicy, _ *admissionv1.ValidatingAdmissionPolicyBinding) {
			p.ResourceVersion = ""
		}},
		{"generation_zero", func(p *admissionv1.ValidatingAdmissionPolicy, _ *admissionv1.ValidatingAdmissionPolicyBinding) {
			p.Generation, p.Status.ObservedGeneration = 0, 0
		}},
		{"generation_negative", func(p *admissionv1.ValidatingAdmissionPolicy, _ *admissionv1.ValidatingAdmissionPolicyBinding) {
			p.Generation, p.Status.ObservedGeneration = -1, -1
		}},
		{"observed_older", func(p *admissionv1.ValidatingAdmissionPolicy, _ *admissionv1.ValidatingAdmissionPolicyBinding) {
			p.Status.ObservedGeneration--
		}},
		{"observed_newer", func(p *admissionv1.ValidatingAdmissionPolicy, _ *admissionv1.ValidatingAdmissionPolicyBinding) {
			p.Status.ObservedGeneration++
		}},
		{"missing_type_checking", func(p *admissionv1.ValidatingAdmissionPolicy, _ *admissionv1.ValidatingAdmissionPolicyBinding) {
			p.Status.TypeChecking = nil
		}},
		{"expression_warning", func(p *admissionv1.ValidatingAdmissionPolicy, _ *admissionv1.ValidatingAdmissionPolicyBinding) {
			p.Status.TypeChecking.ExpressionWarnings = []admissionv1.ExpressionWarning{{FieldRef: "spec.validations[0]", Warning: "synthetic warning"}}
		}},
		{"binding_uid", func(_ *admissionv1.ValidatingAdmissionPolicy, b *admissionv1.ValidatingAdmissionPolicyBinding) {
			b.UID = ""
		}},
		{"binding_version", func(_ *admissionv1.ValidatingAdmissionPolicy, b *admissionv1.ValidatingAdmissionPolicyBinding) {
			b.ResourceVersion = ""
		}},
		{"policy_failure_mode", func(p *admissionv1.ValidatingAdmissionPolicy, _ *admissionv1.ValidatingAdmissionPolicyBinding) {
			ignore := admissionv1.Ignore
			p.Spec.FailurePolicy = &ignore
		}},
		{"policy_expression", func(p *admissionv1.ValidatingAdmissionPolicy, _ *admissionv1.ValidatingAdmissionPolicyBinding) {
			p.Spec.Validations[0].Expression = "false"
		}},
		{"policy_pointer_presence", func(p *admissionv1.ValidatingAdmissionPolicy, _ *admissionv1.ValidatingAdmissionPolicyBinding) {
			p.Spec.MatchConstraints = &admissionv1.MatchResources{}
		}},
		{"binding_policy", func(_ *admissionv1.ValidatingAdmissionPolicy, b *admissionv1.ValidatingAdmissionPolicyBinding) {
			b.Spec.PolicyName = "foreign"
		}},
		{"binding_actions", func(_ *admissionv1.ValidatingAdmissionPolicy, b *admissionv1.ValidatingAdmissionPolicyBinding) {
			b.Spec.ValidationActions = []admissionv1.ValidationAction{admissionv1.Warn}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy, binding, failures := admissionFixture()
			test.change(policy, binding)
			client := fake.NewClientset(policy, binding)
			calls := 0
			err := Check(t.Context(), client.AdmissionregistrationV1(), "fixture", func() (map[string]any, map[string]any) {
				calls++
				if len(client.Actions()) != 2 {
					t.Fatal("source ran before both reads")
				}
				return expectedResources()
			}, failures)
			if test.name == "accepted" {
				if err != nil {
					t.Fatal("valid observed admission was rejected")
				}
			} else if err != failures.Conflict {
				t.Fatal("conflict identity changed")
			}
			if calls != 1 {
				t.Fatal("source invocation count changed")
			}
			actions := client.Actions()
			if len(actions) != 2 {
				t.Fatal("unexpected extra read or write")
			}
			for i, resource := range []string{"validatingadmissionpolicies", "validatingadmissionpolicybindings"} {
				get, ok := actions[i].(k8stesting.GetAction)
				if !ok || get.GetVerb() != "get" || get.GetResource().Resource != resource || get.GetName() != "fixture" || get.GetNamespace() != "" {
					t.Fatal("exact cluster-scoped read order changed")
				}
			}
		})
	}
}

// TestCheckReadAndSourceFailures pins read-before-source precedence and exact caller errors,
// including source marshal/type failures before observed-status rejection. Partial read payloads
// never override a read failure; no retry or publication operation is allowed.
// TestCheckReadAndSourceFailures закрепляет приоритет чтения перед source и точные caller errors,
// включая ошибки marshal/type source до отказа observed status. Partial read payload не заменяет
// ошибку чтения; retry и publication operations не допускаются.
func TestCheckReadAndSourceFailures(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"policy_read", "binding_read", "policy_marshal", "binding_marshal", "policy_decode", "binding_decode"} {
		t.Run(name, func(t *testing.T) {
			policy, binding, failures := admissionFixture()
			policy.Status.TypeChecking = nil
			client := fake.NewClientset(policy, binding)
			resource := ""
			if name == "policy_read" {
				resource = "validatingadmissionpolicies"
			}
			if name == "binding_read" {
				resource = "validatingadmissionpolicybindings"
			}
			if resource != "" {
				client.PrependReactor("get", resource, func(k8stesting.Action) (bool, runtime.Object, error) {
					if name == "binding_read" {
						return true, binding.DeepCopy(), errors.New("synthetic private provider failure")
					}
					return true, policy.DeepCopy(), errors.New("synthetic private provider failure")
				})
			}
			calls := 0
			err := Check(t.Context(), client.AdmissionregistrationV1(), "fixture", func() (map[string]any, map[string]any) {
				calls++
				p, b := expectedResources()
				switch name {
				case "policy_marshal":
					p["invalid"] = make(chan int)
				case "binding_marshal":
					b["invalid"] = make(chan int)
				case "policy_decode":
					p["spec"] = 7
				case "binding_decode":
					b["spec"] = 7
				}
				return p, b
			}, failures)
			wantCalls, wantReads, wantError := 1, 2, failures.Invalid
			if resource != "" {
				wantCalls, wantError = 0, failures.Unavailable
			}
			if name == "policy_read" {
				wantReads = 1
			}
			if calls != wantCalls || len(client.Actions()) != wantReads || err != wantError {
				t.Fatal("failure priority or read/source count changed")
			}
		})
	}
}

// TestCheckInvalidConfiguration rejects absent server dependencies/errors before any API read.
// TestCheckInvalidConfiguration отклоняет отсутствующие server dependencies/errors до API reads.
func TestCheckInvalidConfiguration(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"client", "source", "invalid_error", "unavailable_error", "conflict_error"} {
		t.Run(name, func(t *testing.T) {
			p, b, failures := admissionFixture()
			client := fake.NewClientset(p, b)
			reader := client.AdmissionregistrationV1()
			source := expectedResources
			switch name {
			case "client":
				reader = nil
			case "source":
				source = nil
			case "invalid_error":
				failures.Invalid = nil
			case "unavailable_error":
				failures.Unavailable = nil
			case "conflict_error":
				failures.Conflict = nil
			}
			if Check(t.Context(), reader, "fixture", source, failures) != errInvalidConfiguration || len(client.Actions()) != 0 {
				t.Fatal("invalid configuration reached API reads")
			}
		})
	}
}

// TestSameJSONEncodingBoundary checks serialization failure and nil/empty/type distinctions;
// it does not substitute normalization or Kubernetes semantic equality for byte equality.
// TestSameJSONEncodingBoundary проверяет ошибки сериализации и различия nil/empty/types;
// normalization или Kubernetes semantic equality вместо byte equality не подставляет.
func TestSameJSONEncodingBoundary(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		left, right any
		equal       bool
	}{
		{nil, nil, true}, {[]string(nil), []string{}, false}, {map[string]string(nil), map[string]string{}, false},
		{map[string]int{"a": 1, "b": 2}, map[string]int{"b": 2, "a": 1}, true},
		{1, "1", false}, {true, false, false}, {make(chan int), nil, false}, {nil, make(chan int), false},
	} {
		if got := SameJSON(test.left, test.right); got != test.equal {
			t.Fatal("JSON encoding boundary changed")
		}
	}
	left := []string{"original"}
	before := append([]string(nil), left...)
	if !SameJSON(left, before) || !reflect.DeepEqual(left, before) {
		t.Fatal("comparison changed source values")
	}
}
