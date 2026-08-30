package azureprovider

import (
	"context"
	"crypto/sha256"
	"diffractllm/internal/core"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

func (ap *AzureProvider) AuthInjection(rctx *core.DiffractLLMContext, cred *core.Credential, headers map[string]string) error {
	if cred == nil || cred.Settings.Azure == nil {
		return fmt.Errorf("azure settings not found")
	}
	// Entra modes call rctx.Context(), so this must be checked like cred is.
	if rctx == nil {
		return fmt.Errorf("azure : request context is required")
	}

	settings := cred.Settings.Azure
	switch settings.AuthMode {
	case core.AzureAuthKeyMode:
		if cred.APIKey == "" {
			return fmt.Errorf("azure : api key is required for %s", settings.AuthMode)
		}
		headers["api-key"] = cred.APIKey
		return nil
	case core.AzureAuthServicePrincipal, core.AzureDefaultCredential:
		token, err := ap.azureTokenCredential(rctx.Context(), cred.Settings.Azure)
		if err != nil {
			return err
		}
		headers["Authorization"] = "Bearer " + token
		return nil
	default:
		return fmt.Errorf("unknow setting found for azure provider")
	}

}

func (ap *AzureProvider) azureTokenCredential(ctx context.Context, settings *core.AzureSettings) (string, error) {
	sumkey := sha256.Sum256([]byte(strings.Join([]string{string(settings.AuthMode), settings.TenantID, settings.ClientID, settings.ClientSecret, strings.Join(settings.Scopes, ",")}, "\x00")))
	key := hex.EncodeToString(sumkey[:])

	var tokenCred azcore.TokenCredential
	cached, ok := ap.CredentialCache.Load(key)

	if !ok {
		switch settings.AuthMode {
		case core.AzureAuthServicePrincipal:
			serviceTokenCred, err := azidentity.NewClientSecretCredential(settings.TenantID, settings.ClientID, settings.ClientSecret, nil)
			if err != nil {
				return "", err
			}

			tokenCred = serviceTokenCred
		case core.AzureDefaultCredential:
			// DefaultAzureCredential only reads AZURE_CLIENT_ID from the env, so
			// a configured client id needs managed identity built directly.
			if settings.ClientID != "" {
				userAssignedCred, err := azidentity.NewManagedIdentityCredential(&azidentity.ManagedIdentityCredentialOptions{
					ID: azidentity.ClientID(settings.ClientID),
				})
				if err != nil {
					return "", err
				}
				tokenCred = userAssignedCred
				break
			}
			defaultTokenCred, err := azidentity.NewDefaultAzureCredential(nil)
			if err != nil {
				return "", err
			}
			tokenCred = defaultTokenCred
		default:
			return "", fmt.Errorf("azure-provider : %q unknown settings found", settings.AuthMode)
		}

		// Building a credential makes no network call, so a racing build is
		// harmless - keep whichever landed first and its warm token cache.
		actual, _ := ap.CredentialCache.LoadOrStore(key, tokenCred)
		tokenCred = actual.(azcore.TokenCredential)
	} else {
		tokenCred = cached.(azcore.TokenCredential)
	}

	var scopes []string
	if len(settings.Scopes) > 0 {
		scopes = settings.Scopes
	} else {
		scopes = []string{core.DefaultAzureScope}
	}

	token, err := tokenCred.GetToken(ctx, policy.TokenRequestOptions{Scopes: scopes})
	if err != nil {
		return "", fmt.Errorf("azure : acquiring entra token : %w", err)
	}

	return token.Token, nil
}
