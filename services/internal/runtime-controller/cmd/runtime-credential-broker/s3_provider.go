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
	minioidentity "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/adapters/s3credential"
	port "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/repository/s3credential"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/security/s3policy"
	"k8s.io/client-go/kubernetes"
)

type s3CredentialBackend string

const (
	directProductionPrototypeProfile = "direct-production-single-node-prototype"
	s3CredentialBackendVaultAWS      = s3CredentialBackend("vault-aws")
	s3CredentialBackendInternalMinIO = s3CredentialBackend("internal-minio-service-account")
)

type s3CredentialProvider = port.Provider

func selectS3CredentialBackend(profile, value string) (s3CredentialBackend, error) {
	backend := s3CredentialBackend(value)
	switch backend {
	case s3CredentialBackendVaultAWS:
		if profile == directProductionPrototypeProfile {
			return "", errors.New("direct-production prototype cannot use Vault AWS credential backend")
		}
	case s3CredentialBackendInternalMinIO:
		if profile != directProductionPrototypeProfile {
			return "", errors.New("internal MinIO credential backend requires exact deployment profile")
		}
	default:
		return "", errors.New("runtime S3 credential backend is not registered")
	}
	return backend, nil
}

func newS3CredentialProvider(client kubernetes.Interface, namespace string, action port.Action) (s3CredentialProvider, error) {
	backend, err := configuredS3CredentialBackend()
	if err != nil {
		return nil, err
	}
	if backend == s3CredentialBackendInternalMinIO {
		if client == nil || namespace == "" || !action.Valid() {
			return nil, errors.New("runtime MinIO provider Kubernetes boundary is invalid")
		}
		return minioidentity.New(minioidentity.Config{
			Action: action, Namespace: namespace,
			Endpoint: requiredEnv("RUNTIME_S3_MINIO_ADMIN_ENDPOINT"), TLSServerName: requiredEnv("RUNTIME_S3_MINIO_ADMIN_TLS_SERVER_NAME"),
			CAFile: requiredEnv("RUNTIME_S3_CA_FILE"), AccessKeyIDFile: s3MinioManagementAccessKeyFile,
			SecretAccessKeyFile: s3MinioManagementSecretKeyFile, SigningKeyFile: s3MinioIdentitySigningKeyFile,
			ParentUser: requiredEnv("RUNTIME_S3_MINIO_PARENT_USER"), ParentProfile: "runtime-s3-" + string(action) + "-minio-management",
			Bucket: requiredEnv("RUNTIME_S3_BUCKET"), Region: requiredEnv("RUNTIME_S3_REGION"),
			KMSKeyARN: requiredEnvOrFile("RUNTIME_S3_KMS_KEY_ARN", s3KMSKeyARNFile), KMSKeyID: requiredEnv("RUNTIME_S3_MINIO_KMS_KEY_ID"),
			RequestTimeout: 5 * time.Second,
		}, client.CoreV1().Secrets(namespace))
	}
	vaultClient, address, err := newVaultClient()
	if err != nil {
		return nil, err
	}
	return &vaultAWSS3CredentialProvider{client: vaultClient, address: address}, nil
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

func (provider *vaultAWSS3CredentialProvider) Issue(ctx context.Context, request port.Request) (port.Issue, error) {
	if !request.Action.Valid() || request.Execution.ID == "" ||
		request.SourceExecutionID == "" || len(request.PolicyRaw) == 0 || len(request.PolicyRaw) > 2048 {
		return port.Issue{}, errors.New("runtime S3 credential provider request is invalid")
	}
	kubernetesJWT, err := os.ReadFile(vaultTokenFile)
	if err != nil || len(kubernetesJWT) < 20 || len(kubernetesJWT) > 1<<20 {
		return port.Issue{}, errors.New("read runtime S3 broker identity")
	}
	login := vaultResponse{}
	vaultRole := "runtime-s3-" + string(request.Action) + "-exchanger"
	if err := vaultRequest(ctx, provider.client, http.MethodPost, provider.address+"/v1/auth/kubernetes/login", "", map[string]any{
		"role": vaultRole, "jwt": string(kubernetesJWT),
	}, &login); err != nil || login.Auth == nil || login.Auth.ClientToken == "" || login.Auth.Accessor == "" {
		return port.Issue{}, errors.New("authenticate exact runtime S3 broker identity")
	}
	tags := map[string]string{
		"organization_id": request.Execution.OrganizationID, "project_id": request.Execution.ProjectID,
		"session_id": request.Execution.SessionID, "execution_id": request.Execution.ID,
		"source_execution_id": request.SourceExecutionID,
	}
	if request.Action == port.ActionRestore {
		_, archiveReference := s3policy.RestoreArchiveSource(request.Execution)
		tags["archive_version_id"] = s3policy.ExactVersionID(archiveReference)
	}
	bootstrap := vaultResponse{}
	if err := vaultRequest(ctx, provider.client, http.MethodPost, provider.address+"/v1/aws/sts/runtime-"+string(request.Action)+"-exchanger",
		login.Auth.ClientToken, map[string]any{
			"ttl": "15m", "role_session_name": "mcx-broker-" + shortID(request.Execution.ID) + "-" + string(request.Action),
		}, &bootstrap); err != nil {
		return port.Issue{}, errors.New("issue runtime S3 broker credential")
	}
	bootstrapAccessKey, accessOK := bootstrap.Data["access_key"].(string)
	bootstrapSecretKey, secretOK := bootstrap.Data["secret_key"].(string)
	bootstrapToken, tokenOK := bootstrap.Data["security_token"].(string)
	if !accessOK || !secretOK || !tokenOK || bootstrapAccessKey == "" || bootstrapSecretKey == "" || bootstrapToken == "" ||
		bootstrap.LeaseID == "" || bootstrap.LeaseDuration < 60 || bootstrap.LeaseDuration > int64(maximumSTSTTL/time.Second) {
		return port.Issue{}, errors.New("runtime S3 broker credential response is invalid")
	}
	stsClient, roleARN, err := newS3STSClient(string(request.Action), bootstrapAccessKey, bootstrapSecretKey, bootstrapToken)
	if err != nil {
		return port.Issue{}, err
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
	sessionName := "mcx-" + shortID(request.Execution.ID) + "-" + string(request.Action)
	duration := int32(maximumSTSTTL / time.Second)
	assumed, err := stsClient.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn: aws.String(roleARN), RoleSessionName: aws.String(sessionName), DurationSeconds: &duration,
		Policy: aws.String(string(request.PolicyRaw)), Tags: tagValues,
	})
	if err != nil || assumed.Credentials == nil || assumed.AssumedRoleUser == nil {
		return port.Issue{}, errors.New("assume exact runtime S3 execution role")
	}
	issued := port.Issue{
		AccessKeyID: aws.ToString(assumed.Credentials.AccessKeyId), SecretAccessKey: aws.ToString(assumed.Credentials.SecretAccessKey),
		SessionToken: aws.ToString(assumed.Credentials.SessionToken), ExpiresAt: aws.ToTime(assumed.Credentials.Expiration).UTC(),
		BootstrapLeaseID: bootstrap.LeaseID, LoginAccessor: login.Auth.Accessor,
		AssumedRoleARN: aws.ToString(assumed.AssumedRoleUser.Arn), SessionName: sessionName,
		RevocationToken: login.Auth.ClientToken,
	}
	if issued.AccessKeyID == "" || issued.SecretAccessKey == "" || issued.SessionToken == "" || issued.AssumedRoleARN == "" ||
		issued.ExpiresAt.Before(time.Now().UTC().Add(time.Minute)) || issued.ExpiresAt.After(time.Now().UTC().Add(maximumSTSTTL+time.Minute)) {
		return port.Issue{}, errors.New("runtime S3 credential response is invalid")
	}
	return issued, nil
}

func (provider *vaultAWSS3CredentialProvider) Check(context.Context, port.Request) error {
	if provider == nil || provider.client == nil || provider.address == "" {
		return errors.New("runtime Vault AWS credential provider is unavailable")
	}
	return nil
}

func (provider *vaultAWSS3CredentialProvider) Ready(ctx context.Context, action port.Action) error {
	return provider.Check(ctx, port.Request{Action: action})
}

func (provider *vaultAWSS3CredentialProvider) Revoke(ctx context.Context, _ port.Request, issued port.Issue) error {
	if issued.BootstrapLeaseID == "" || issued.RevocationToken == "" || strings.ContainsAny(issued.BootstrapLeaseID, "\x00\r\n") {
		return errors.New("runtime S3 bootstrap lease revoke input is invalid")
	}
	if err := vaultRequest(ctx, provider.client, http.MethodPost, provider.address+"/v1/sys/leases/revoke",
		issued.RevocationToken, map[string]string{"lease_id": issued.BootstrapLeaseID}, nil); err != nil {
		return errors.New("revoke runtime S3 bootstrap lease")
	}
	return nil
}

func (provider *vaultAWSS3CredentialProvider) Close() { provider.client.CloseIdleConnections() }
