package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSecureVerificationTest(t *testing.T) (service.AuthIdentity, string) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousSessionSecret := common.SessionSecret
	previousRedis := common.RedisEnabled
	previousSelfUse := operation_setting.SelfUseModeEnabled
	previousDemo := operation_setting.DemoSiteEnabled
	model.DB = db
	model.LOG_DB = db
	common.SessionSecret = "secure-verification-test-session-secret"
	common.RedisEnabled = false
	operation_setting.SelfUseModeEnabled = true
	operation_setting.DemoSiteEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.SessionSecret = previousSessionSecret
		common.RedisEnabled = previousRedis
		operation_setting.SelfUseModeEnabled = previousSelfUse
		operation_setting.DemoSiteEnabled = previousDemo
	})

	privateKeyPEM, err := common.GeneratePasswordEncryptionPrivateKey()
	require.NoError(t, err)
	require.NoError(t, common.LoadPasswordEncryptionPrivateKey(privateKeyPEM))

	passwordHash, err := common.Password2Hash("owner-password")
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "root", Password: passwordHash,
		Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1,
	}).Error)

	return service.AuthIdentity{
		UserID:          1,
		SessionID:       "secure-verification-session",
		UserAuthVersion: 1,
		SessionVersion:  1,
	}, passwordHash
}

func encryptSecureVerificationPassword(t *testing.T, password string) (string, string) {
	t.Helper()
	keyID, publicKeyPEM := common.PasswordEncryptionPublicKey()
	require.NotEmpty(t, keyID)
	require.NotEmpty(t, publicKeyPEM)

	block, rest := pem.Decode([]byte(publicKeyPEM))
	require.NotNil(t, block)
	require.Empty(t, strings.TrimSpace(string(rest)))
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	require.NoError(t, err)
	publicKey, ok := parsed.(*rsa.PublicKey)
	require.True(t, ok)

	ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, []byte(password), nil)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(ciphertext), keyID
}

func performSecureVerification(t *testing.T, identity service.AuthIdentity, role int, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/verify", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set("id", identity.UserID)
	context.Set("role", role)
	context.Set("session_id", identity.SessionID)
	context.Set("auth_version", identity.UserAuthVersion)
	context.Set("session_version", identity.SessionVersion)
	UniversalVerify(context)
	return recorder
}

func secureVerificationBody(t *testing.T, password, scope, method string) string {
	t.Helper()
	encrypted, keyID := encryptSecureVerificationPassword(t, password)
	body, err := common.Marshal(map[string]string{
		"method":             method,
		"password_encrypted": encrypted,
		"encryption_key_id":  keyID,
		"scope":              scope,
	})
	require.NoError(t, err)
	return string(body)
}

func TestUniversalVerifyUsesAdministratorPasswordAndSession(t *testing.T) {
	identity, _ := setupSecureVerificationTest(t)

	recorder := performSecureVerification(t, identity, common.RoleRootUser,
		secureVerificationBody(t, "owner-password", securityProofScopeChannelKeyRead, secureVerificationMethodPassword))
	assert.Equal(t, http.StatusOK, recorder.Code)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			ProofToken string `json:"proof_token"`
			Method     string `json:"method"`
			Scope      string `json:"scope"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, secureVerificationMethodPassword, response.Data.Method)
	assert.Equal(t, securityProofScopeChannelKeyRead, response.Data.Scope)
	method, err := service.VerifySecurityProof(response.Data.ProofToken, identity, securityProofScopeChannelKeyRead, []string{secureVerificationMethodPassword})
	require.NoError(t, err)
	assert.Equal(t, secureVerificationMethodPassword, method)
}

func TestUniversalVerifyRejectsInvalidPasswordMethodScopeAndCredential(t *testing.T) {
	identity, _ := setupSecureVerificationTest(t)
	tests := []struct {
		name string
		role int
		body string
	}{
		{
			name: "wrong password",
			role: common.RoleRootUser,
			body: secureVerificationBody(t, "wrong-password", securityProofScopeChannelKeyRead, secureVerificationMethodPassword),
		},
		{
			name: "wrong method",
			role: common.RoleRootUser,
			body: secureVerificationBody(t, "owner-password", securityProofScopeChannelKeyRead, "2fa"),
		},
		{
			name: "wrong scope",
			role: common.RoleRootUser,
			body: secureVerificationBody(t, "owner-password", "passkey.delete", secureVerificationMethodPassword),
		},
		{
			name: "common user role",
			role: common.RoleCommonUser,
			body: secureVerificationBody(t, "owner-password", securityProofScopeChannelKeyRead, secureVerificationMethodPassword),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := performSecureVerification(t, identity, test.role, test.body)
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Contains(t, recorder.Body.String(), `"success":false`)
		})
	}

	patIdentity := identity
	patIdentity.SessionID = ""
	recorder := performSecureVerification(t, patIdentity, common.RoleRootUser,
		secureVerificationBody(t, "owner-password", securityProofScopeChannelKeyRead, secureVerificationMethodPassword))
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}
