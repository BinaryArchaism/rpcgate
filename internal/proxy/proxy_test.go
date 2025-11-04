package proxy_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/BinaryArchaism/rpcgate/internal/proxy"
)

func Test_Server_getBasicAuthDecoded(t *testing.T) {
	getBase64Encoded := func(str string) string {
		return base64.StdEncoding.EncodeToString([]byte(str))
	}
	testCases := []struct {
		name     string
		header   string
		login    string
		password string
		needErr  bool
	}{
		{
			name:     "ok",
			header:   "Basic " + getBase64Encoded("admin:test"),
			login:    "admin",
			password: "test",
			needErr:  false,
		},
		{
			name:     "ok without prefix",
			header:   getBase64Encoded("admin:test"),
			login:    "admin",
			password: "test",
			needErr:  false,
		},
		{
			name:     "ok without pass",
			header:   "Basic " + getBase64Encoded("admin:"),
			login:    "admin",
			password: "",
			needErr:  false,
		},
		{
			name:     "corrupted base64",
			header:   "=corrupted",
			login:    "",
			password: "",
			needErr:  true,
		},
		{
			name:     "empty",
			header:   "Basic " + getBase64Encoded(":"),
			login:    "_unknown_",
			password: "",
			needErr:  false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			login, password, err := proxy.GetBasicAuthDecoded(tc.header)
			if tc.needErr {
				require.Error(t, err)
				return
			}
			require.Equal(t, tc.login, login)
			require.Equal(t, tc.password, password)
		})
	}
}
