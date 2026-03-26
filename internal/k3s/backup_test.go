package k3s

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ── rewriteKubeconfigInsecure ─────────────────────────────────────────────────

func TestRewriteKubeconfigInsecure_RemovesCertData(t *testing.T) {
	input := `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: dGVzdC1jZXJ0
    server: https://127.0.0.1:6443
  name: default
`
	out := rewriteKubeconfigInsecure(input)

	if strings.Contains(out, "certificate-authority-data") {
		t.Error("expected certificate-authority-data to be removed")
	}
	if !strings.Contains(out, "insecure-skip-tls-verify: true") {
		t.Error("expected insecure-skip-tls-verify: true to be present")
	}
	// Server address should be unchanged
	if !strings.Contains(out, "server: https://127.0.0.1:6443") {
		t.Error("server address should be unchanged")
	}
}

func TestRewriteKubeconfigInsecure_NoDoubleInsert(t *testing.T) {
	// If there are two clusters (unlikely but defensive), we should insert
	// insecure-skip-tls-verify once per certificate-authority-data occurrence.
	input := `clusters:
- cluster:
    certificate-authority-data: abc123
    server: https://1.2.3.4:6443
- cluster:
    certificate-authority-data: def456
    server: https://5.6.7.8:6443
`
	out := rewriteKubeconfigInsecure(input)
	count := strings.Count(out, "insecure-skip-tls-verify")
	if count != 2 {
		t.Errorf("expected 2 insecure-skip-tls-verify lines, got %d", count)
	}
}

func TestRewriteKubeconfigInsecure_Idempotent(t *testing.T) {
	// Calling it on output that has no cert data should be a no-op.
	input := `clusters:
- cluster:
    insecure-skip-tls-verify: true
    server: https://127.0.0.1:6443
`
	out := rewriteKubeconfigInsecure(input)
	if out != input {
		t.Errorf("expected no change, got:\n%s", out)
	}
}

func TestRewriteKubeconfigInsecure_PreservesIndent(t *testing.T) {
	// The replacement line must carry the same indentation as the original.
	input := "    certificate-authority-data: abc\n"
	out := rewriteKubeconfigInsecure(input)
	if !strings.HasPrefix(out, "    insecure-skip-tls-verify") {
		t.Errorf("indentation not preserved: %q", out)
	}
}

// ── cleanForBackup ────────────────────────────────────────────────────────────

func TestCleanForBackup_RemovesRuntimeFields(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":            "my-app",
				"namespace":       "default",
				"resourceVersion": "12345",
				"uid":             "abc-def",
				"generation":      int64(3),
				"managedFields":   []interface{}{"something"},
				"annotations": map[string]interface{}{
					"kubectl.kubernetes.io/last-applied": "{}",
					"app.example.com/config":             "keep-me",
				},
			},
			"status": map[string]interface{}{
				"readyReplicas": 1,
			},
		},
	}

	cleanForBackup(obj)

	if obj.GetResourceVersion() != "" {
		t.Error("resourceVersion should be cleared")
	}
	if obj.GetUID() != "" {
		t.Error("uid should be cleared")
	}
	if obj.GetGeneration() != 0 {
		t.Error("generation should be cleared")
	}
	if obj.GetManagedFields() != nil {
		t.Error("managedFields should be cleared")
	}
	if _, ok := obj.Object["status"]; ok {
		t.Error("status should be removed")
	}

	anns := obj.GetAnnotations()
	if _, ok := anns["kubectl.kubernetes.io/last-applied"]; ok {
		t.Error("kubectl last-applied annotation should be removed")
	}
	if anns["app.example.com/config"] != "keep-me" {
		t.Error("other annotations should be preserved")
	}
}

func TestCleanForBackup_PreservesName(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "my-service",
				"namespace": "production",
			},
		},
	}
	cleanForBackup(obj)
	if obj.GetName() != "my-service" {
		t.Errorf("name should be preserved, got %q", obj.GetName())
	}
	if obj.GetNamespace() != "production" {
		t.Errorf("namespace should be preserved, got %q", obj.GetNamespace())
	}
}
