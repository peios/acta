package client

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"time"
)

// HarnessAccount retries availability failures before local process ownership is
// opened. Authentication, redirects and malformed responses remain terminal.
func (c *Client) HarnessAccount(ctx context.Context, hostname string, report func(HarnessState) error) (Account, error) {
	delay := time.Second
	for {
		account, err := c.Account(ctx)
		if err == nil {
			if account.ID == "" {
				return Account{}, errors.New("Acta returned an account without an identity")
			}
			return account, nil
		}
		if ctx.Err() != nil {
			return Account{}, ctx.Err()
		}
		var apiErr *Error
		var networkErr net.Error
		retry := errors.As(err, &networkErr) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
		if errors.As(err, &apiErr) {
			retry = apiErr.Status == 429 || apiErr.Status >= 500
		}
		if !retry {
			return Account{}, err
		}
		if err = waitHarnessRetry(ctx, hostname, delay, report); err != nil {
			return Account{}, err
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func waitHarnessRetry(ctx context.Context, hostname string, delay time.Duration, report func(HarnessState) error) error {
	wait := delay/2 + time.Duration(rand.Int64N(int64(delay/2)+1))
	if err := report(HarnessState{State: "reconnecting", Hostname: hostname, RetryIn: wait.Seconds()}); err != nil {
		return err
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
