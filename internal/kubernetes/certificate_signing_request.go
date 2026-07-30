package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	certificateSigningRequestCollectionPath                = "/apis/certificates.k8s.io/v1/certificatesigningrequests"
	certificateSigningRequestListPageSize                  = "250"
	certificateSigningRequestMaxListPages                  = 4
	certificateSigningRequestMaxListItems                  = 1000
	certificateSigningRequestMaxListBytes            int64 = 4 * 1024 * 1024
	certificateSigningRequestMaxDetailBytes          int64 = 2 * 1024 * 1024
	certificateSigningRequestMaxContinueBytes              = 16 * 1024
	certificateSigningRequestMaxSignerBytes                = 512
	certificateSigningRequestMaxSignerPathBytes            = 253
	certificateSigningRequestMaxRequesterBytes             = 256
	certificateSigningRequestMaxUsages                     = 32
	certificateSigningRequestMaxConditions                 = 32
	certificateSigningRequestMaxConditionTypeBytes         = 128
	certificateSigningRequestMaxConditionReasonBytes       = 256
)

var certificateSigningRequestUsages = map[string]struct{}{
	"signing": {}, "digital signature": {}, "content commitment": {}, "key encipherment": {},
	"key agreement": {}, "data encipherment": {}, "cert sign": {}, "crl sign": {},
	"encipher only": {}, "decipher only": {}, "any": {}, "server auth": {}, "client auth": {},
	"code signing": {}, "email protection": {}, "s/mime": {}, "ipsec end system": {},
	"ipsec tunnel": {}, "ipsec user": {}, "timestamping": {}, "ocsp signing": {},
	"microsoft sgc": {}, "netscape sgc": {},
}

type certificateSigningRequestConditionSource struct {
	Type               string     `json:"type"`
	Status             string     `json:"status"`
	Reason             string     `json:"reason"`
	LastUpdateTime     *time.Time `json:"lastUpdateTime"`
	LastTransitionTime *time.Time `json:"lastTransitionTime"`
}

type certificateSigningRequestSource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name              string    `json:"name"`
		Namespace         string    `json:"namespace"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		SignerName        string   `json:"signerName"`
		ExpirationSeconds *int32   `json:"expirationSeconds"`
		Usages            []string `json:"usages"`
		Username          string   `json:"username"`
	} `json:"spec"`
	Status struct {
		Certificate certificateSigningRequestCertificateSource `json:"certificate"`
		Conditions  []certificateSigningRequestConditionSource `json:"conditions"`
	} `json:"status"`
}

type certificateSigningRequestCertificateSource bool

func (source *certificateSigningRequestCertificateSource) UnmarshalJSON(value []byte) error {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' || !json.Valid(value) {
		return fmt.Errorf("invalid Kubernetes CSR certificate encoding")
	}
	*source = len(value) > 2
	return nil
}

func (c *Client) CertificateSigningRequests(ctx context.Context) ([]domain.KubernetesCertificateSigningRequest, error) {
	query := url.Values{"limit": {certificateSigningRequestListPageSize}}
	items := make([]domain.KubernetesCertificateSigningRequest, 0)
	seenNames := make(map[string]struct{})
	seenContinue := make(map[string]struct{})
	var totalBytes int64
	for page := 0; page < certificateSigningRequestMaxListPages; page++ {
		remainingBytes := certificateSigningRequestMaxListBytes - totalBytes
		if remainingBytes <= 0 {
			return nil, fmt.Errorf("Kubernetes CSR list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		payload, _, err := c.getPayload(
			ctx, certificateSigningRequestCollectionPath, query, kubernetesPartialMetadataListAccept, remainingBytes, false,
		)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(payload))

		var response partialObjectMetadataList
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes CSR metadata list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "PartialObjectMetadataList" {
			return nil, fmt.Errorf("unsupported Kubernetes CSR metadata list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > certificateSigningRequestMaxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes CSR list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		for _, raw := range response.Items {
			metadata, err := decodePartialObjectMetadataForScope(raw, false)
			if err != nil {
				return nil, err
			}
			if domain.ValidateCertificateSigningRequestName(metadata.Name) != nil {
				return nil, fmt.Errorf("invalid Kubernetes CSR metadata identity: %w", domain.ErrUpstream)
			}
			if _, duplicate := seenNames[metadata.Name]; duplicate {
				return nil, fmt.Errorf("duplicate Kubernetes CSR metadata identity: %w", domain.ErrUpstream)
			}
			seenNames[metadata.Name] = struct{}{}
			items = append(items, domain.KubernetesCertificateSigningRequest{
				Name: metadata.Name, CreatedAt: metadata.CreationTimestamp.UTC(),
			})
		}

		continuation := response.Metadata.Continue
		if continuation == "" {
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			return items, nil
		}
		if !validCertificateSigningRequestContinue(continuation) {
			return nil, fmt.Errorf("invalid Kubernetes CSR continuation token: %w", domain.ErrUpstream)
		}
		if _, duplicate := seenContinue[continuation]; duplicate {
			return nil, fmt.Errorf("repeated Kubernetes CSR continuation token: %w", domain.ErrUpstream)
		}
		seenContinue[continuation] = struct{}{}
		query.Set("continue", continuation)
	}
	return nil, fmt.Errorf("Kubernetes CSR list exceeded safe page limit: %w", domain.ErrUpstream)
}

func (c *Client) CertificateSigningRequest(
	ctx context.Context,
	name string,
) (domain.KubernetesCertificateSigningRequestDetail, error) {
	if err := domain.ValidateCertificateSigningRequestName(name); err != nil {
		return domain.KubernetesCertificateSigningRequestDetail{}, err
	}
	payload, _, err := c.getPayload(
		ctx, certificateSigningRequestCollectionPath+"/"+name, nil, "application/json",
		certificateSigningRequestMaxDetailBytes, false,
	)
	if err != nil {
		return domain.KubernetesCertificateSigningRequestDetail{}, err
	}
	return decodeCertificateSigningRequest(payload, name)
}

func decodeCertificateSigningRequest(
	payload []byte,
	expectedName string,
) (domain.KubernetesCertificateSigningRequestDetail, error) {
	var source certificateSigningRequestSource
	if err := json.Unmarshal(payload, &source); err != nil {
		return domain.KubernetesCertificateSigningRequestDetail{}, fmt.Errorf("decode Kubernetes CSR detail: %w", domain.ErrUpstream)
	}
	if source.APIVersion != "certificates.k8s.io/v1" || source.Kind != "CertificateSigningRequest" ||
		source.Metadata.Name != expectedName || source.Metadata.Namespace != "" || source.Metadata.CreationTimestamp.IsZero() ||
		domain.ValidateCertificateSigningRequestName(source.Metadata.Name) != nil {
		return domain.KubernetesCertificateSigningRequestDetail{}, fmt.Errorf("invalid Kubernetes CSR identity: %w", domain.ErrUpstream)
	}
	if !validCertificateSigningRequestSignerName(source.Spec.SignerName) {
		return domain.KubernetesCertificateSigningRequestDetail{}, fmt.Errorf("invalid Kubernetes CSR signer: %w", domain.ErrUpstream)
	}
	if !validCertificateSigningRequestText(source.Spec.Username, certificateSigningRequestMaxRequesterBytes, true) {
		return domain.KubernetesCertificateSigningRequestDetail{}, fmt.Errorf("invalid Kubernetes CSR requester: %w", domain.ErrUpstream)
	}
	if source.Spec.ExpirationSeconds != nil && *source.Spec.ExpirationSeconds < 600 {
		return domain.KubernetesCertificateSigningRequestDetail{}, fmt.Errorf("invalid Kubernetes CSR requested expiration: %w", domain.ErrUpstream)
	}
	usages, err := decodeCertificateSigningRequestUsages(source.Spec.Usages)
	if err != nil {
		return domain.KubernetesCertificateSigningRequestDetail{}, err
	}
	conditions, approved, denied, failed, err := decodeCertificateSigningRequestConditions(source.Status.Conditions)
	if err != nil {
		return domain.KubernetesCertificateSigningRequestDetail{}, err
	}
	if approved && denied {
		return domain.KubernetesCertificateSigningRequestDetail{}, fmt.Errorf("conflicting Kubernetes CSR approval conditions: %w", domain.ErrUpstream)
	}
	certificateIssued := bool(source.Status.Certificate)
	if certificateIssued && (!approved || denied || failed) {
		return domain.KubernetesCertificateSigningRequestDetail{}, fmt.Errorf("invalid Kubernetes CSR issued state: %w", domain.ErrUpstream)
	}

	state := domain.CertificateSigningRequestPending
	switch {
	case denied:
		state = domain.CertificateSigningRequestDenied
	case failed:
		state = domain.CertificateSigningRequestFailed
	case certificateIssued:
		state = domain.CertificateSigningRequestIssued
	case approved:
		state = domain.CertificateSigningRequestApproved
	}

	return domain.KubernetesCertificateSigningRequestDetail{
		KubernetesCertificateSigningRequest: domain.KubernetesCertificateSigningRequest{
			Name: source.Metadata.Name, CreatedAt: source.Metadata.CreationTimestamp.UTC(),
		},
		Requester: source.Spec.Username, SignerName: source.Spec.SignerName,
		RequestedExpirationSeconds: source.Spec.ExpirationSeconds, Usages: usages, State: state,
		CertificateIssued: certificateIssued, Conditions: conditions, ConditionCount: len(source.Status.Conditions),
	}, nil
}

func decodeCertificateSigningRequestUsages(source []string) ([]string, error) {
	if len(source) == 0 || len(source) > certificateSigningRequestMaxUsages {
		return nil, fmt.Errorf("Kubernetes CSR usages exceeded safe limit: %w", domain.ErrUpstream)
	}
	seen := make(map[string]struct{}, len(source))
	usages := append([]string(nil), source...)
	for _, usage := range usages {
		if _, valid := certificateSigningRequestUsages[usage]; !valid {
			return nil, fmt.Errorf("invalid Kubernetes CSR usage: %w", domain.ErrUpstream)
		}
		if _, duplicate := seen[usage]; duplicate {
			return nil, fmt.Errorf("duplicate Kubernetes CSR usage: %w", domain.ErrUpstream)
		}
		seen[usage] = struct{}{}
	}
	sort.Strings(usages)
	return usages, nil
}

func decodeCertificateSigningRequestConditions(
	source []certificateSigningRequestConditionSource,
) ([]domain.KubernetesCertificateSigningRequestCondition, bool, bool, bool, error) {
	if len(source) > certificateSigningRequestMaxConditions {
		return nil, false, false, false, fmt.Errorf("Kubernetes CSR conditions exceeded safe limit: %w", domain.ErrUpstream)
	}
	conditions := make([]domain.KubernetesCertificateSigningRequestCondition, 0, len(source))
	seen := make(map[string]struct{}, len(source))
	approved, denied, failed := false, false, false
	for _, condition := range source {
		if !validCertificateSigningRequestText(condition.Type, certificateSigningRequestMaxConditionTypeBytes, true) ||
			!validCertificateSigningRequestText(condition.Reason, certificateSigningRequestMaxConditionReasonBytes, false) ||
			(condition.Status != "True" && condition.Status != "False" && condition.Status != "Unknown") ||
			(condition.LastUpdateTime != nil && condition.LastUpdateTime.IsZero()) ||
			(condition.LastTransitionTime != nil && condition.LastTransitionTime.IsZero()) {
			return nil, false, false, false, fmt.Errorf("invalid Kubernetes CSR condition: %w", domain.ErrUpstream)
		}
		if _, duplicate := seen[condition.Type]; duplicate {
			return nil, false, false, false, fmt.Errorf("duplicate Kubernetes CSR condition: %w", domain.ErrUpstream)
		}
		seen[condition.Type] = struct{}{}
		known := condition.Type == "Approved" || condition.Type == "Denied" || condition.Type == "Failed"
		if known && condition.Status != "True" {
			return nil, false, false, false, fmt.Errorf("invalid Kubernetes CSR known condition status: %w", domain.ErrUpstream)
		}
		approved = approved || condition.Type == "Approved"
		denied = denied || condition.Type == "Denied"
		failed = failed || condition.Type == "Failed"
		conditions = append(conditions, domain.KubernetesCertificateSigningRequestCondition{
			Type: condition.Type, Status: condition.Status, Reason: condition.Reason,
			LastUpdateTime:     normalizedCertificateSigningRequestTime(condition.LastUpdateTime),
			LastTransitionTime: normalizedCertificateSigningRequestTime(condition.LastTransitionTime),
		})
	}
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].Type < conditions[j].Type })
	return conditions, approved, denied, failed, nil
}

func normalizedCertificateSigningRequestTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

func validCertificateSigningRequestSignerName(value string) bool {
	if value == "" || len(value) > certificateSigningRequestMaxSignerBytes || value != strings.TrimSpace(value) ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	prefix, signerPath, found := strings.Cut(value, "/")
	if !found || strings.Contains(signerPath, "/") ||
		domain.ValidateAdmissionPolicyResourceName(prefix) != nil ||
		len(signerPath) == 0 || len(signerPath) > certificateSigningRequestMaxSignerPathBytes {
		return false
	}
	for index, character := range signerPath {
		alphanumeric := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9'
		if alphanumeric {
			continue
		}
		if index == 0 || index == len(signerPath)-1 || (character != '-' && character != '_' && character != '.') {
			return false
		}
	}
	return true
}

func validCertificateSigningRequestContinue(value string) bool {
	return value != "" && len(value) <= certificateSigningRequestMaxContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func validCertificateSigningRequestText(value string, maxBytes int, required bool) bool {
	if value == "" {
		return !required
	}
	return len(value) <= maxBytes && utf8.ValidString(value) && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, func(character rune) bool { return !unicode.IsPrint(character) }) < 0
}
