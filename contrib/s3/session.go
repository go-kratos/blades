package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-kratos/blades"
)

var _ blades.SessionStore = (*SessionStore)(nil)

// SessionStore implements a session store using AWS S3 as the backend.
type SessionStore struct {
	bucket string
	client *s3.Client
}

// NewSessionStore creates a new SessionStore with the given S3 bucket and AWS configuration.
func NewSessionStore(bucket string, cfg aws.Config, opts ...func(*s3.Options)) (*SessionStore, error) {
	client := s3.NewFromConfig(cfg, opts...)
	return &SessionStore{
		bucket: bucket,
		client: client,
	}, nil
}

// GetSession retrieves a session by its ID from the S3 bucket.
func (s *SessionStore) GetSession(ctx context.Context, id string) (*blades.Session, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(id),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var session blades.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &session, nil
}

// SaveSession saves a session to the S3 bucket.
func (s *SessionStore) SaveSession(ctx context.Context, session *blades.Session) error {
	body, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(session.ID),
		Body:   bytes.NewReader(body),
	})
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	return nil
}

// DeleteSession deletes a session by its ID from the S3 bucket.
func (s *SessionStore) DeleteSession(ctx context.Context, id string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(id),
	})
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}
