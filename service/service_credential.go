package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	server "github.com/FlyTOmeLight/server-go"
	"github.com/FlyTOmeLight/server-go/conf"
	"github.com/FlyTOmeLight/server-go/db"
	gormlogger "gorm.io/gorm/logger"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/credential"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
)

// ServiceCredentialAdmin issues, lists and revokes the API keys backend
// services present on internal calls. A token is printed once, at issue time;
// the database only keeps its digest.
func ServiceCredentialAdmin(ctx context.Context, output io.Writer, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: server service-credential <issue|list|revoke> ...")
	}
	snapshot, err := conf.Load("")
	if err != nil {
		return err
	}
	file, err := config.Decode(snapshot)
	if err != nil {
		return err
	}
	c, err := file.Normalize()
	if err != nil {
		return err
	}
	if err := c.RequireDatabase(); err != nil {
		return err
	}
	database := &db.Module{Config: &c.DB}
	resources := server.NewBootstrap(database)
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = resources.Close(closeCtx)
	}()
	startup, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := resources.Initialize(startup); err != nil {
		return err
	}
	database.DB.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	operation, cancelOperation := context.WithTimeout(ctx, 30*time.Second)
	defer cancelOperation()
	credentials := credential.New(database.DB)
	encode := json.NewEncoder(output)
	encode.SetIndent("", "  ")
	switch args[0] {
	case "ensure":
		if len(args) < 3 || len(args) > 5 {
			return errors.New("usage: server service-credential ensure SERVICE TOKEN_FILE [NAME] [EXPIRES_AT]")
		}
		raw, err := os.ReadFile(args[2])
		if err != nil {
			return errors.New("cannot read service credential file")
		}
		name, expiry := "default", ""
		if len(args) > 3 {
			name = args[3]
		}
		if len(args) > 4 {
			expiry = args[4]
		}
		item, err := credentials.EnsureService(operation, args[1], name, strings.TrimSpace(string(raw)), expiry)
		if err != nil {
			return err
		}
		return encode.Encode(item)
	case "issue":
		if len(args) < 2 || len(args) > 4 {
			return errors.New("usage: server service-credential issue SERVICE [NAME] [EXPIRES_AT]")
		}
		name := "default"
		if len(args) > 2 {
			name = args[2]
		}
		expiry := ""
		if len(args) > 3 {
			expiry = args[3]
		}
		expiresAt, err := credential.Expiry(expiry, 365*24*time.Hour)
		if err != nil {
			return err
		}
		issued, err := credentials.IssueService(operation, args[1], name, expiresAt)
		if err != nil {
			return err
		}
		return encode.Encode(issued)
	case "list":
		if len(args) != 2 {
			return errors.New("usage: server service-credential list SERVICE")
		}
		items, err := credentials.ListService(operation, args[1])
		if err != nil {
			return err
		}
		return encode.Encode(items)
	case "revoke":
		if len(args) != 2 {
			return errors.New("usage: server service-credential revoke CREDENTIAL_ID")
		}
		revoked, err := credentials.RevokeService(operation, args[1])
		if err != nil {
			return err
		}
		if !revoked {
			return fmt.Errorf("credential %s does not exist or is already revoked", args[1])
		}
		return encode.Encode(map[string]any{"id": args[1], "revoked": true})
	default:
		return fmt.Errorf("unknown service-credential command %q", args[0])
	}
}
