// Copyright (c) 2015-2022 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/minio/cli"
	json "github.com/minio/colorjson"
	"github.com/minio/madmin-go/v2"
	"github.com/minio/mc/pkg/probe"
	"github.com/minio/pkg/console"
	iampolicy "github.com/minio/pkg/iam/policy"
)

var adminUserSvcAcctAddFlags = []cli.Flag{
	cli.StringFlag{
		Name:  "access-key",
		Usage: "set an access key for the service account",
	},
	cli.StringFlag{
		Name:  "secret-key",
		Usage: "set a secret key for the service account",
	},
	cli.StringFlag{
		Name:  "policy",
		Usage: "path to a JSON policy file",
	},
	cli.StringFlag{
		Name:  "comment",
		Usage: "personal note for the service account",
	},
}

var adminUserSvcAcctAddCmd = cli.Command{
	Name:         "add",
	Usage:        "add a new service account",
	Action:       mainAdminUserSvcAcctAdd,
	OnUsageError: onUsageError,
	Before:       setGlobalsFromContext,
	Flags:        append(adminUserSvcAcctAddFlags, globalFlags...),
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} ALIAS ACCOUNT

ACCOUNT:
  An account could be a regular MinIO user, STS ou LDAP user.

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Add a new service account for user 'foobar' to MinIO server.
     {{.Prompt}} {{.HelpName}} myminio foobar
`,
}

// checkAdminUserSvcAcctAddSyntax - validate all the passed arguments
func checkAdminUserSvcAcctAddSyntax(ctx *cli.Context) {
	if len(ctx.Args()) != 2 {
		showCommandHelpAndExit(ctx, 1)
	}
}

// acctMessage container for content message structure
type acctMessage struct {
	op            acctOp
	Status        string          `json:"status"`
	AccessKey     string          `json:"accessKey,omitempty"`
	SecretKey     string          `json:"secretKey,omitempty"`
	ParentUser    string          `json:"parentUser,omitempty"`
	ImpliedPolicy bool            `json:"impliedPolicy,omitempty"`
	Policy        json.RawMessage `json:"policy,omitempty"`
	Comment       string          `json:"comment,omitempty"`
	AccountStatus string          `json:"accountStatus,omitempty"`
	MemberOf      []string        `json:"memberOf,omitempty"`
}

const (
	accessFieldMaxLen = 20
)

type acctOp int

const (
	svcAccOpAdd = acctOp(iota)
	svcAccOpList
	svcAccOpInfo
	svcAccOpRemove
	svcAccOpDisable
	svcAccOpEnable
	svcAccOpSet

	stsAccOpInfo
)

func (u acctMessage) String() string {
	switch u.op {
	case svcAccOpList:
		// Create a new pretty table with cols configuration
		return newPrettyTable("  ",
			Field{"AccessKey", accessFieldMaxLen},
		).buildRow(u.AccessKey)
	case stsAccOpInfo, svcAccOpInfo:
		policyField := ""
		if u.ImpliedPolicy {
			policyField = "implied"
		} else {
			policyField = "embedded"
		}
		return console.Colorize("AccMessage", strings.Join(
			[]string{
				fmt.Sprintf("AccessKey: %s", u.AccessKey),
				fmt.Sprintf("ParentUser: %s", u.ParentUser),
				fmt.Sprintf("Status: %s", u.AccountStatus),
				fmt.Sprintf("Comment: %s", u.Comment),
				fmt.Sprintf("Policy: %s", policyField),
			}, "\n"))
	case svcAccOpRemove:
		return console.Colorize("AccMessage", "Removed service account `"+u.AccessKey+"` successfully.")
	case svcAccOpDisable:
		return console.Colorize("AccMessage", "Disabled service account `"+u.AccessKey+"` successfully.")
	case svcAccOpEnable:
		return console.Colorize("AccMessage", "Enabled service account `"+u.AccessKey+"` successfully.")
	case svcAccOpAdd:
		return console.Colorize("AccMessage",
			fmt.Sprintf("Access Key: %s\nSecret Key: %s", u.AccessKey, u.SecretKey))
	case svcAccOpSet:
		return console.Colorize("AccMessage", "Edited service account `"+u.AccessKey+"` successfully.")
	}
	return ""
}

func (u acctMessage) JSON() string {
	u.Status = "success"
	jsonMessageBytes, e := json.MarshalIndent(u, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")

	return string(jsonMessageBytes)
}

// mainAdminUserSvcAcctAdd is the handle for "mc admin user svcacct add" command.
func mainAdminUserSvcAcctAdd(ctx *cli.Context) error {
	checkAdminUserSvcAcctAddSyntax(ctx)

	console.SetColor("AccMessage", color.New(color.FgGreen))

	// Get the alias parameter from cli
	args := ctx.Args()
	aliasedURL := args.Get(0)
	user := args.Get(1)

	accessKey := ctx.String("access-key")
	secretKey := ctx.String("secret-key")
	policyPath := ctx.String("policy")
	comment := ctx.String("comment")

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	var policyBytes []byte
	if policyPath != "" {
		// Validate the policy document and ensure it has at least when statement
		var e error
		policyBytes, e = os.ReadFile(policyPath)
		fatalIf(probe.NewError(e), "Unable to open the policy document.")
		p, e := iampolicy.ParseConfig(bytes.NewReader(policyBytes))
		fatalIf(probe.NewError(e), "Unable to parse the policy document.")
		if p.IsEmpty() {
			fatalIf(errInvalidArgument(), "Empty policy documents are not allowed.")
		}
	}

	opts := madmin.AddServiceAccountReq{
		Policy:     policyBytes,
		AccessKey:  accessKey,
		SecretKey:  secretKey,
		Comment:    comment,
		TargetUser: user,
	}

	creds, e := client.AddServiceAccount(globalContext, opts)
	fatalIf(probe.NewError(e).Trace(args...), "Unable to add a new service account")

	printMsg(acctMessage{
		op:            svcAccOpAdd,
		AccessKey:     creds.AccessKey,
		SecretKey:     creds.SecretKey,
		AccountStatus: "enabled",
	})

	return nil
}
