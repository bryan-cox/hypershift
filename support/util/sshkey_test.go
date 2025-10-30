package util

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExtractSSHPublicKey(t *testing.T) {
	tests := []struct {
		name          string
		secret        *corev1.Secret
		expectedKey   string
		expectedData  string
		expectError   bool
		errorContains string
	}{
		{
			name: "When id_rsa.pub exists it should return the key and data",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
				Data: map[string][]byte{
					"id_rsa.pub": []byte("ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ..."),
				},
			},
			expectedKey:  "id_rsa.pub",
			expectedData: "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ...",
			expectError:  false,
		},
		{
			name: "When id_ecdsa.pub exists it should return the key and data",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
				Data: map[string][]byte{
					"id_ecdsa.pub": []byte("ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTY..."),
				},
			},
			expectedKey:  "id_ecdsa.pub",
			expectedData: "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTY...",
			expectError:  false,
		},
		{
			name: "When id_ed25519.pub exists it should return the key and data",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
				Data: map[string][]byte{
					"id_ed25519.pub": []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIN..."),
				},
			},
			expectedKey:  "id_ed25519.pub",
			expectedData: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIN...",
			expectError:  false,
		},
		{
			name: "When custom .pub key exists it should return the key and data",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
				Data: map[string][]byte{
					"my-custom-key.pub": []byte("ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ..."),
				},
			},
			expectedKey:  "my-custom-key.pub",
			expectedData: "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ...",
			expectError:  false,
		},
		{
			name: "When id_rsa.pub exists with other .pub keys it should prioritize id_rsa.pub",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
				Data: map[string][]byte{
					"id_rsa.pub":     []byte("ssh-rsa RSA_KEY"),
					"id_ecdsa.pub":   []byte("ecdsa-sha2-nistp256 ECDSA_KEY"),
					"id_ed25519.pub": []byte("ssh-ed25519 ED25519_KEY"),
				},
			},
			expectedKey:  "id_rsa.pub",
			expectedData: "ssh-rsa RSA_KEY",
			expectError:  false,
		},
		{
			name: "When no .pub key exists it should return an error",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
				Data: map[string][]byte{
					"id_rsa": []byte("PRIVATE_KEY"),
					"config": []byte("some-config"),
				},
			},
			expectError:   true,
			errorContains: "must contain a public SSH key",
		},
		{
			name: "When secret is empty it should return an error",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
				Data:       map[string][]byte{},
			},
			expectError:   true,
			errorContains: "must contain a public SSH key",
		},
		{
			name: "When .pub key exists but is empty it should search for other .pub keys",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
				Data: map[string][]byte{
					"empty.pub":  []byte(""),
					"id_rsa.pub": []byte("ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ..."),
				},
			},
			expectedKey:  "id_rsa.pub",
			expectedData: "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ...",
			expectError:  false,
		},
		{
			name: "When all .pub keys are empty it should return an error",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
				Data: map[string][]byte{
					"id_rsa.pub":   []byte(""),
					"id_ecdsa.pub": []byte(""),
				},
			},
			expectError:   true,
			errorContains: "must contain a public SSH key",
		},
		{
			name:          "When secret is nil it should return an error",
			secret:        nil,
			expectError:   true,
			errorContains: "secret is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyName, data, err := ExtractSSHPublicKey(tt.secret)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if keyName != tt.expectedKey {
				t.Errorf("expected key name %q, got %q", tt.expectedKey, keyName)
			}

			if string(data) != tt.expectedData {
				t.Errorf("expected data %q, got %q", tt.expectedData, string(data))
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
