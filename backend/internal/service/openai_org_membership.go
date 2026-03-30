package service

import "strings"

func isOpenAINoOrganizationError(statusCode int, responseBody []byte) bool {
	if statusCode != 401 {
		return false
	}

	if strings.EqualFold(strings.TrimSpace(extractUpstreamErrorCode(responseBody)), "no_organization") {
		return true
	}

	msg := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(responseBody)))
	if msg == "" {
		return false
	}

	return strings.Contains(msg, "must be a member of an organization") ||
		strings.Contains(msg, "member of an organization to use the api")
}

func buildOpenAINoOrganizationTempUnschedReason(responseBody []byte) string {
	msg := strings.TrimSpace(extractUpstreamErrorMessage(responseBody))
	if msg == "" {
		return "OpenAI organization membership pending (401): account is not ready for API access yet"
	}
	return "OpenAI organization membership pending (401): " + sanitizeUpstreamErrorMessage(msg)
}
