// Package gmail provides a thin interface over the Gmail API so the core logic
// can be tested without live API calls.
package gmail

import (
	"context"
	"net/http"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Message holds just the fields the spam counter needs from gmail.Message.
type Message struct {
	ID           string
	InternalDate int64
}

// MessageRef is a lightweight reference returned by list calls.
type MessageRef struct {
	ID string
}

// ListResponse is the result of listing spam messages.
type ListResponse struct {
	Messages      []MessageRef
	NextPageToken string
}

// Client is the interface used by the spam counting service.
type Client interface {
	ListSpamMessages(ctx context.Context, query string, pageToken string) (*ListResponse, error)
	GetMessage(ctx context.Context, id string) (*Message, error)
}

// NewServiceClient builds a Client backed by the real Gmail API.
func NewServiceClient(ctx context.Context, httpClient *http.Client) (Client, error) {
	srv, err := gmail.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return &serviceClient{srv: srv}, nil
}

type serviceClient struct {
	srv *gmail.Service
}

func (c *serviceClient) ListSpamMessages(ctx context.Context, query string, pageToken string) (*ListResponse, error) {
	req := c.srv.Users.Messages.List("me").LabelIds("SPAM").Q(query)
	if pageToken != "" {
		req = req.PageToken(pageToken)
	}

	resp, err := req.Context(ctx).Do()
	if err != nil {
		return nil, err
	}

	refs := make([]MessageRef, len(resp.Messages))
	for i, m := range resp.Messages {
		refs[i] = MessageRef{ID: m.Id}
	}

	return &ListResponse{
		Messages:      refs,
		NextPageToken: resp.NextPageToken,
	}, nil
}

func (c *serviceClient) GetMessage(ctx context.Context, id string) (*Message, error) {
	msg, err := c.srv.Users.Messages.Get("me", id).Format("minimal").Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return &Message{
		ID:           msg.Id,
		InternalDate: msg.InternalDate,
	}, nil
}
