// The api command runs acme-api.
//
// Usage:
//
//	api                              run the API server
//	api openapi                      print the OpenAPI document
//	api roles                        list the platform roles
//	api grant-role <email> <role>    give an account a platform role
//	api revoke-role <email> <role>   take a platform role away
//	api reset-mfa <email>            turn off an account's two-factor authentication
//	api rotate-auth-keys             re-encrypt 2FA secrets with the first AUTH_ENCRYPTION_KEYS key
//	api auth-providers               show which sign-in methods are configured
package main

import (
	"context"
	"fmt"
	"os"

	"apistock.dev/config"

	"example.com/acme-api/internal/app"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "acme-api:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := app.LoadConfig(config.OS)
	if err != nil {
		return err
	}
	if len(args) > 0 {
		switch args[0] {
		case "openapi":
			return app.WriteOpenAPI(ctx, cfg, os.Stdout)
		case "roles":
			app.WriteRoles(os.Stdout)
			return nil
		case "grant-role", "revoke-role":
			if len(args) != 3 {
				return fmt.Errorf("usage: api %s <email> <role>", args[0])
			}
			if args[0] == "grant-role" {
				return app.GrantRole(ctx, cfg, args[1], args[2], os.Stdout)
			}
			return app.RevokeRole(ctx, cfg, args[1], args[2], os.Stdout)
		case "reset-mfa":
			if len(args) != 2 {
				return fmt.Errorf("usage: api reset-mfa <email>")
			}
			return app.ResetMFA(ctx, cfg, args[1], os.Stdout)
		case "rotate-auth-keys":
			return app.RotateAuthKeys(ctx, cfg, os.Stdout)
		case "auth-providers":
			// LoadConfig has already refused an invalid or partial configuration.
			app.WriteSignInMethods(os.Stdout, cfg)
			return nil
		default:
			return fmt.Errorf("unknown command %q", args[0])
		}
	}

	a, err := app.New(ctx, cfg)
	if err != nil {
		return err
	}
	return a.Run(ctx)
}
