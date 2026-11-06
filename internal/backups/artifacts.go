package backups

import "time"

// Destinations are selected from the saved policy, never supplied as paths by
// the caller. Revision binds the position to the configuration being viewed.
type ArtifactQuery struct {
	ID               string `json:"id"`
	Revision         uint64 `json:"revision"`
	DestinationIndex *int   `json:"destination_index"`
}

func ValidateArtifactQuery(q ArtifactQuery) error {
	if !ValidPolicyID(q.ID) || q.Revision == 0 || q.DestinationIndex == nil || *q.DestinationIndex < 0 || *q.DestinationIndex >= 100 {
		return ErrInvalid
	}
	return nil
}

type ArtifactFile struct {
	Object      string    `json:"object"`
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	BackupAt    time.Time `json:"backup_at"`
	Bytes       *int64    `json:"bytes,omitempty"`
	SizeDisplay string    `json:"size_display,omitempty"`
}
type ArtifactList struct {
	Items     []ArtifactFile `json:"items"`
	CheckedAt time.Time      `json:"checked_at"`
	Truncated bool           `json:"truncated"`
	Warning   string         `json:"warning,omitempty"`
}
