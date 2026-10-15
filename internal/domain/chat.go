package domain

const (
	ChatRoleUser      = "user"
	ChatRoleAssistant = "assistant"
)

// ChatMessage — реплика диалога с ботом.
type ChatMessage struct {
	Role    string
	Content string
}
