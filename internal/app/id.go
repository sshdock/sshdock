package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func ReleaseID(appID string, commitSHA string) string {
	return "rel_" + appID + "_" + commitSHA
}

func eventID(subject string, eventType string) string {
	return "evt_" + sanitizeEventID(subject) + "_" + sanitizeEventID(eventType)
}

func restartOperationID(appID string, serviceName string, now time.Time) string {
	return fmt.Sprintf("%s_%s_%d", appID, serviceName, now.UTC().UnixNano())
}

func sanitizeEventID(value string) string {
	result := make([]rune, 0, len(value))
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			result = append(result, char)
		case char >= 'A' && char <= 'Z':
			result = append(result, char)
		case char >= '0' && char <= '9':
			result = append(result, char)
		default:
			result = append(result, '_')
		}
	}
	return string(result)
}

func NewDeploymentID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate deployment attempt ID: %w", err)
	}

	return "dep_" + hex.EncodeToString(value[:]), nil
}
