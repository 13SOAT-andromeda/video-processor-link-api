package link

import "fmt"

// Status representa o estado do link na máquina de estados (spec §4).
type Status string

const (
	StatusLinkCreated         Status = "LINK_CREATED"
	StatusUploadPending       Status = "UPLOAD_PENDING"
	StatusUploadCompleted     Status = "UPLOAD_COMPLETED"
	StatusProcessingPending   Status = "PROCESSING_PENDING"
	StatusProcessingStarted   Status = "PROCESSING_STARTED"
	StatusProcessingCompleted Status = "PROCESSING_COMPLETED"
	StatusUploadFailed        Status = "UPLOAD_FAILED"
	StatusProcessingFailed    Status = "PROCESSING_FAILED"
)

// validTransitions define a máquina de estados. O links-service é o único
// escritor (ADR-007): toda transição passa por aqui.
var validTransitions = map[Status][]Status{
	StatusLinkCreated:         {StatusUploadPending, StatusUploadCompleted, StatusUploadFailed},
	StatusUploadPending:       {StatusUploadCompleted, StatusUploadFailed},
	StatusUploadCompleted:     {StatusProcessingPending, StatusProcessingStarted, StatusProcessingFailed},
	StatusProcessingPending:   {StatusProcessingStarted, StatusProcessingFailed},
	StatusProcessingStarted:   {StatusProcessingCompleted, StatusProcessingFailed},
	StatusProcessingCompleted: {},
	StatusUploadFailed:        {},
	StatusProcessingFailed:    {},
}

// CanTransition informa se a transição from -> to é válida.
func CanTransition(from, to Status) bool {
	for _, next := range validTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// IsTerminal informa se o status é final.
func IsTerminal(s Status) bool {
	return len(validTransitions[s]) == 0
}

// IsValid informa se o status existe na máquina de estados.
func IsValid(s Status) bool {
	_, ok := validTransitions[s]
	return ok
}

// ErrInvalidTransition é retornado quando a transição não é permitida (409).
type ErrInvalidTransition struct {
	From, To Status
}

func (e ErrInvalidTransition) Error() string {
	return fmt.Sprintf("INVALID_STATUS_TRANSITION: %s -> %s", e.From, e.To)
}
