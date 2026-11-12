"""Закрытая привязка дочернего kubectl к одному target без изменения host config."""

import unittest
from target_kubectl import command


class TargetKubectlTest(unittest.TestCase):
    def setUp(self):
        self.environment = {"KODEX_TARGET_KUBECTL": "/tools/kubectl-real",
                            "KODEX_TARGET_KUBECONFIG": "/private/config",
                            "KODEX_TARGET_CONTEXT": "synthetic-local"}

    def test_exact_flags_are_present_for_every_command(self):
        self.assertEqual(command(["get", "namespaces"], self.environment),
                         ["/tools/kubectl-real", "--kubeconfig", "/private/config",
                          "--context", "synthetic-local", "get", "namespaces"])
        self.assertEqual(command(["--context=synthetic-local", "get", "pods"], self.environment)[-2:],
                         ["get", "pods"])

    def test_other_target_and_host_mutation_are_rejected(self):
        for args in (["--context", "foreign", "get", "pods"], ["--kubeconfig=/foreign", "get", "pods"],
                     ["--server=https://example.invalid", "get", "pods"], ["config", "use-context", "synthetic-local"],
                     ["config", "set", "current-context", "synthetic-local"], ["--context"]):
            with self.subTest(args=args), self.assertRaises(ValueError):
                command(args, self.environment)

    def test_current_context_reads_explicit_selection(self):
        self.assertEqual(command(["config", "current-context"], self.environment)[-5:],
                         ["config", "view", "--minify", "-o", "jsonpath={.contexts[0].name}"])

    def test_exec_payload_is_not_interpreted_as_kubernetes_flags(self):
        arguments = ["exec", "synthetic", "--", "tool", "--context", "business", "-s", "text"]
        self.assertEqual(command(arguments, self.environment)[5:], arguments)


if __name__ == "__main__":
    unittest.main()
