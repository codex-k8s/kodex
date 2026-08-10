package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
)

type s3CredentialBackend string

const (
	directProductionPrototypeProfile = "direct-production-single-node-prototype"
	s3CredentialBackendVaultAWS      = s3CredentialBackend("vault-aws")
	s3CredentialBackendDirectSTS     = s3CredentialBackend("direct-production-s3-sts")
)

type s3CredentialIssue struct {
	AccessKeyID, SecretAccessKey, SessionToken string
	ExpiresAt                                  time.Time
	BootstrapLeaseID, LoginAccessor            string
	AssumedRoleARN, SessionName                string
	revocationToken                            string
}

type s3CredentialRequest struct {
	Execution         entity.Execution
	Action            string
	SourceExecutionID string
	PolicyRaw         []byte
}

type s3CredentialProvider interface {
	Issue(context.Context, s3CredentialRequest) (s3CredentialIssue, error)
	Check(context.Context, string) error
	Revoke(context.Context, s3CredentialIssue) error
	Close()
}

func selectS3CredentialBackend(profile, value string) (s3CredentialBackend, error) {
	backend := s3CredentialBackend(value)
	switch backend {
	case s3CredentialBackendVaultAWS:
		if profile == directProductionPrototypeProfile {
			return "", errors.New("direct-production prototype cannot use Vault AWS credential backend")
		}
	case s3CredentialBackendDirectSTS:
		if profile != directProductionPrototypeProfile {
			return "", errors.New("direct S3 STS backend requires exact deployment profile")
		}
	default:
		return "", errors.New("runtime S3 credential backend is not registered")
	}
	return backend, nil
}

func newS3CredentialProvider() (s3CredentialProvider, error) {
	backend, err := configuredS3CredentialBackend()
	if err != nil {
		return nil, err
	}
	if backend == s3CredentialBackendDirectSTS {
		return nil, errors.New("direct-production S3 STS provider requires an external OIDC or identity-plugin binding")
	}
	client, address, err := newVaultClient()
	if err != nil {
		return nil, err
	}
	return &vaultAWSS3CredentialProvider{client: client, address: address}, nil
}

func configuredS3CredentialBackend() (s3CredentialBackend, error) {
	profile := requiredEnv("RUNTIME_DEPLOYMENT_PROFILE")
	if profile == "" {
		profile = "production"
	}
	value := requiredEnv("RUNTIME_S3_CREDENTIAL_BACKEND")
	if value == "" {
		value = string(s3CredentialBackendVaultAWS)
	}
	backend, err := selectS3CredentialBackend(profile, value)
	if err != nil {
		return "", err
	}
	return backend, nil
}

type vaultAWSS3CredentialProvider struct {
	client  *http.Client
	address string
}

func (provider *vaultAWSS3CredentialProvider) Issue(ctx context.Context, request s3CredentialRequest) (s3CredentialIssue, error) {
	if request.Action != "archive" && request.Action != "restore" || request.Execution.ID == "" ||
		request.SourceExecutionID == "" || len(request.PolicyRaw) == 0 || len(request.PolicyRaw) > 2048 {
		return s3CredentialIssue{}, errors.New("runtime S3 credential provider request is invalid")
	}
	kubernetesJWT, err := os.ReadFile(vaultTokenFile)
	if err != nil || len(kubernetesJWT) < 20 || len(kubernetesJWT) > 1<<20 {
		return s3CredentialIssue{}, errors.New("read runtime S3 broker identity")
	}
	login := vaultResponse{}
	vaultRole := "runtime-s3-" + request.Action + "-exchanger"
	if err := vaultRequest(ctx, provider.client, http.MethodPost, provider.address+"/v1/auth/kubernetes/login", "", map[string]any{
		"role": vaultRole, "jwt": string(kubernetesJWT),
	}, &login); err != nil || login.Auth == nil || login.Auth.ClientToken == "" || login.Auth.Accessor == "" {
		return s3CredentialIssue{}, errors.New("authenticate exact runtime S3 broker identity")
	}
	tags := map[string]string{
		"organization_id": request.Execution.OrganizationID, "project_id": request.Execution.ProjectID,
		"session_id": request.Execution.SessionID, "execution_id": request.Execution.ID,
		"source_execution_id": request.SourceExecutionID,
	}
	if request.Action == "restore" {
		_, archiveReference := restoreArchiveSource(request.Execution)
		tags["archive_version_id"] = exactVersionID(archiveReference)
	}
	bootstrap := vaultResponse{}
	if err := vaultRequest(ctx, provider.client, http.MethodPost, provider.address+"/v1/aws/sts/runtime-"+request.Action+"-exchanger",
		login.Auth.ClientToken, map[string]any{
			"ttl": "15m", "role_session_name": "mcx-broker-" + shortID(request.Execution.ID) + "-" + request.Action,
		}, &bootstrap); err != nil {
		return s3CredentialIssue{}, errors.New("issue runtime S3 broker credential")
	}
	bootstrapAccessKey, accessOK := bootstrap.Data["access_key"].(string)
	bootstrapSecretKey, secretOK := bootstrap.Data["secret_key"].(string)
	bootstrapToken, tokenOK := bootstrap.Data["security_token"].(string)
	if !accessOK || !secretOK || !tokenOK || bootstrapAccessKey == "" || bootstrapSecretKey == "" || bootstrapToken == "" ||
		bootstrap.LeaseID == "" || bootstrap.LeaseDuration < 60 || bootstrap.LeaseDuration > int64(maximumSTSTTL/time.Second) {
		return s3CredentialIssue{}, errors.New("runtime S3 broker credential response is invalid")
	}
	stsClient, roleARN, err := newS3STSClient(request.Action, bootstrapAccessKey, bootstrapSecretKey, bootstrapToken)
	if err != nil {
		return s3CredentialIssue{}, err
	}
	tagNames := make([]string, 0, len(tags))
	for name := range tags {
		tagNames = append(tagNames, name)
	}
	sort.Strings(tagNames)
	tagValues := make([]ststypes.Tag, 0, len(tagNames))
	for _, name := range tagNames {
		tagValues = append(tagValues, ststypes.Tag{Key: aws.String(name), Value: aws.String(tags[name])})
	}
	sessionName := "mcx-" + shortID(request.Execution.ID) + "-" + request.Action
	duration := int32(maximumSTSTTL / time.Second)
	assumed, err := stsClient.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn: aws.String(roleARN), RoleSessionName: aws.String(sessionName), DurationSeconds: &duration,
		Policy: aws.String(string(request.PolicyRaw)), Tags: tagValues,
	})
	if err != nil || assumed.Credentials == nil || assumed.AssumedRoleUser == nil {
		return s3CredentialIssue{}, errors.New("assume exact runtime S3 execution role")
	}
	issued := s3CredentialIssue{
		AccessKeyID: aws.ToString(assumed.Credentials.AccessKeyId), SecretAccessKey: aws.ToString(assumed.Credentials.SecretAccessKey),
		SessionToken: aws.ToString(assumed.Credentials.SessionToken), ExpiresAt: aws.ToTime(assumed.Credentials.Expiration).UTC(),
		BootstrapLeaseID: bootstrap.LeaseID, LoginAccessor: login.Auth.Accessor,
		AssumedRoleARN: aws.ToString(assumed.AssumedRoleUser.Arn), SessionName: sessionName,
		revocationToken: login.Auth.ClientToken,
	}
	if issued.AccessKeyID == "" || issued.SecretAccessKey == "" || issued.SessionToken == "" || issued.AssumedRoleARN == "" ||
		issued.ExpiresAt.Before(time.Now().UTC().Add(time.Minute)) || issued.ExpiresAt.After(time.Now().UTC().Add(maximumSTSTTL+time.Minute)) {
		return s3CredentialIssue{}, errors.New("runtime S3 credential response is invalid")
	}
	return issued, nil
}

func (provider *vaultAWSS3CredentialProvider) Check(context.Context, string) error {
	if provider == nil || provider.client == nil || provider.address == "" {
		return errors.New("runtime Vault AWS credential provider is unavailable")
	}
	return nil
}

func (provider *vaultAWSS3CredentialProvider) Revoke(ctx context.Context, issued s3CredentialIssue) error {
	if issued.BootstrapLeaseID == "" || issued.revocationToken == "" || strings.ContainsAny(issued.BootstrapLeaseID, "\x00\r\n") {
		return errors.New("runtime S3 bootstrap lease revoke input is invalid")
	}
	if err := vaultRequest(ctx, provider.client, http.MethodPost, provider.address+"/v1/sys/leases/revoke",
		issued.revocationToken, map[string]string{"lease_id": issued.BootstrapLeaseID}, nil); err != nil {
		return errors.New("revoke runtime S3 bootstrap lease")
	}
	return nil
}

func (provider *vaultAWSS3CredentialProvider) Close() { provider.client.CloseIdleConnections() }
