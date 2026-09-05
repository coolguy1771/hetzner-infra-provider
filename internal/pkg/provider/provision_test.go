// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"errors"
	"testing"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

func TestIsAlreadyGoneErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "not found",
			err:  hcloud.Error{Code: hcloud.ErrorCodeNotFound, Message: "server not found"},
			want: true,
		},
		{
			name: "other api error",
			err:  hcloud.Error{Code: hcloud.ErrorCodeRateLimitExceeded, Message: "rate limited"},
			want: false,
		},
		{
			name: "wrapped not found",
			err:  errors.Join(errors.New("context"), hcloud.Error{Code: hcloud.ErrorCodeNotFound}),
			want: true,
		},
		{
			name: "generic error",
			err:  errors.New("boom"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAlreadyGoneErr(tt.err); got != tt.want {
				t.Errorf("isAlreadyGoneErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestBoolDefault(t *testing.T) {
	trueVal, falseVal := true, false

	if !boolDefault(nil, true) {
		t.Error("boolDefault(nil, true) = false, want true")
	}

	if boolDefault(nil, false) {
		t.Error("boolDefault(nil, false) = true, want false")
	}

	if boolDefault(&falseVal, true) {
		t.Error("boolDefault(&false, true) = true, want false")
	}

	if !boolDefault(&trueVal, false) {
		t.Error("boolDefault(&true, false) = false, want true")
	}
}
