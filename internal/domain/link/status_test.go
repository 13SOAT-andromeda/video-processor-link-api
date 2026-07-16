package link

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanTransition(t *testing.T) {
	cases := []struct {
		from, to Status
		want     bool
	}{
		{StatusLinkCreated, StatusUploadCompleted, true},
		{StatusLinkCreated, StatusUploadPending, true},
		{StatusLinkCreated, StatusUploadFailed, true},
		{StatusUploadPending, StatusUploadCompleted, true},
		{StatusUploadCompleted, StatusProcessingPending, true},
		{StatusUploadCompleted, StatusProcessingStarted, true},
		{StatusProcessingPending, StatusProcessingStarted, true},
		{StatusProcessingStarted, StatusProcessingCompleted, true},
		{StatusProcessingStarted, StatusProcessingFailed, true},
		// inválidas
		{StatusLinkCreated, StatusProcessingCompleted, false},
		{StatusProcessingCompleted, StatusProcessingStarted, false},
		{StatusProcessingFailed, StatusProcessingStarted, false},
		{StatusUploadFailed, StatusUploadCompleted, false},
		{StatusProcessingCompleted, StatusLinkCreated, false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, CanTransition(c.from, c.to), "%s -> %s", c.from, c.to)
	}
}

func TestIsTerminal(t *testing.T) {
	assert.True(t, IsTerminal(StatusProcessingCompleted))
	assert.True(t, IsTerminal(StatusProcessingFailed))
	assert.True(t, IsTerminal(StatusUploadFailed))
	assert.False(t, IsTerminal(StatusLinkCreated))
	assert.False(t, IsTerminal(StatusProcessingStarted))
}

func TestLinkTransition(t *testing.T) {
	now := time.Now()
	l := New("u-1", "video.mp4", false, now)
	require.Equal(t, StatusLinkCreated, l.Status)
	require.Equal(t, l.LinkID+"/raw/video.mp4", l.S3RawKey)
	require.Equal(t, now.AddDate(0, 0, RetentionDays).Unix(), l.ExpiresAt)

	ev, err := l.Transition(StatusUploadCompleted, "UPLOAD_CALLBACK", nil, now)
	require.NoError(t, err)
	assert.Equal(t, StatusLinkCreated, ev.StatusFrom)
	assert.Equal(t, StatusUploadCompleted, ev.StatusTo)
	assert.Equal(t, StatusUploadCompleted, l.Status)

	_, err = l.Transition(StatusProcessingCompleted, "X", nil, now)
	var invalid ErrInvalidTransition
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, StatusUploadCompleted, l.Status, "status não muda em transição inválida")
}

func TestOwnedBy(t *testing.T) {
	l := New("u-1", "v.mp4", true, time.Now())
	assert.True(t, l.OwnedBy("u-1"))
	assert.False(t, l.OwnedBy("u-2"))
}
