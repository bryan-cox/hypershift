package util

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	// DefaultSSHKeyName is the default SSH public key name for backward compatibility
	DefaultSSHKeyName = "id_rsa.pub"
)

// ExtractSSHPublicKey extracts SSH public key data from a secret.
// It looks for any key ending with ".pub" suffix (e.g., "id_rsa.pub", "id_ecdsa.pub", "id_ed25519.pub").
// For backward compatibility, it prioritizes "id_rsa.pub" if present.
// If multiple .pub keys exist, it returns the first one found.
// Returns the key name and data, or an error if no valid SSH public key is found.
func ExtractSSHPublicKey(secret *corev1.Secret) (string, []byte, error) {
	if secret == nil {
		return "", nil, fmt.Errorf("secret is nil")
	}

	// First, check for the default key name for backward compatibility
	if data, ok := secret.Data[DefaultSSHKeyName]; ok && len(data) > 0 {
		return DefaultSSHKeyName, data, nil
	}

	// Look for any key ending with .pub suffix
	for key, data := range secret.Data {
		if strings.HasSuffix(key, ".pub") && len(data) > 0 {
			return key, data, nil
		}
	}

	// No valid SSH public key found
	return "", nil, fmt.Errorf("secret %q must contain a public SSH key (a key ending with '.pub', e.g., 'id_rsa.pub', 'id_ecdsa.pub', 'id_ed25519.pub')", secret.Name)
}
