// Package fake provides an in-memory Gmail client for testing the spam service.
package fake

import (
	"context"
	"sync"

	"github.com/sidecut/gocheckspam/internal/gmail"
)

// Client is a thread-safe fake gmail.Client.
type Client struct {
	mu sync.Mutex

	pages     []Page
	pageIndex int
	messages  map[string]*gmail.Message
	getCalls  []string
	listCalls []ListCall
}

// Page represents one page of list results.
type Page struct {
	Messages      []gmail.MessageRef
	NextPageToken string
	Err           error
}

// ListCall captures a single ListSpamMessages invocation.
type ListCall struct {
	Query     string
	PageToken string
}

// NewClient returns a fake client with no data.
func NewClient() *Client {
	return &Client{
		messages: make(map[string]*gmail.Message),
	}
}

// WithPages configures the paged list responses.
func (c *Client) WithPages(pages []Page) *Client {
	c.pages = pages
	return c
}

// WithMessage registers a message that can be fetched by ID.
func (c *Client) WithMessage(id string, msg *gmail.Message) *Client {
	c.messages[id] = msg
	return c
}

// ListSpamMessages returns the next configured page.
func (c *Client) ListSpamMessages(_ context.Context, query string, pageToken string) (*gmail.ListResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.listCalls = append(c.listCalls, ListCall{Query: query, PageToken: pageToken})

	if c.pageIndex >= len(c.pages) {
		return &gmail.ListResponse{}, nil
	}
	page := c.pages[c.pageIndex]
	c.pageIndex++
	if page.Err != nil {
		return nil, page.Err
	}
	return &gmail.ListResponse{
		Messages:      page.Messages,
		NextPageToken: page.NextPageToken,
	}, nil
}

// GetMessage returns a registered message by ID.
func (c *Client) GetMessage(_ context.Context, id string) (*gmail.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.getCalls = append(c.getCalls, id)
	msg, ok := c.messages[id]
	if !ok {
		return nil, nil
	}
	return msg, nil
}

// GetCalls returns the IDs passed to GetMessage.
func (c *Client) GetCalls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.getCalls...)
}

// ListCalls returns the list invocations.
func (c *Client) ListCalls() []ListCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ListCall(nil), c.listCalls...)
}
