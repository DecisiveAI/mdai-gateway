package utils

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func GetEnvVariableWithDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func HandleHTTPError(w http.ResponseWriter, logger *zap.Logger, code int, message string, err error) {
	if err != nil {
		logger.Error(message, zap.Error(err))
		http.Error(w, message+": "+err.Error(), code)
	} else {
		logger.Warn(message)
		http.Error(w, message, code)
	}
}

func CreateEventUUID() string {
	id := uuid.New()
	return id.String()
}

func GetValkeyExpiry(logger *zap.Logger) time.Duration {
	const defaultExpiry = 30 * 24 * time.Hour
	expiryStr := os.Getenv(valkeyAuditStreamExpiryMSEnvVarKey)
	if expiryStr == "" {
		return defaultExpiry
	}
	expiryMs, err := strconv.Atoi(expiryStr)
	if err != nil {
		logger.Error("Invalid "+valkeyAuditStreamExpiryMSEnvVarKey+" value", zap.Error(err))
		return defaultExpiry
	}
	expiry := time.Duration(expiryMs) * time.Millisecond
	logger.Info("Using custom stream expiration", zap.String("stream", mdaiHubEventHistoryStreamName), zap.Duration("expiry", expiry))
	return expiry
}
