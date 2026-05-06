package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/februality/serverpilot-mcp/internal/spapi"
	mcsh "github.com/februality/serverpilot-mcp/internal/ssh"
)

// RegisterBootstrapTools registers sp_ssh_setup, sp_ssh_status, sp_ssh_remove.
func RegisterBootstrapTools(s *server.MCPServer, d *Deps) {
	s.AddTool(toolSSHSetup(), handleSSHSetup(d))
	s.AddTool(toolSSHStatus(), handleSSHStatus(d))
	s.AddTool(toolSSHRemove(), handleSSHRemove(d))
}

func toolSSHSetup() mcp.Tool {
	return mcp.NewTool("sp_ssh_setup",
		mcp.WithDescription("Generate SSH key, register with ServerPilot, and assign to system users. Safe to re-run — skips users that already have the key."),
		mcp.WithArray("users",
			mcp.Description("Specific system user IDs to assign to. If omitted, assigns to all system users."),
			mcp.Items(map[string]any{"type": "string"}),
		),
	)
}

func toolSSHStatus() mcp.Tool {
	return mcp.NewTool("sp_ssh_status",
		mcp.WithDescription("Show SSH key status: whether the key exists locally and on ServerPilot, and which system users have it assigned"),
	)
}

func toolSSHRemove() mcp.Tool {
	return mcp.NewTool("sp_ssh_remove",
		mcp.WithDescription("Remove the SSH key from specific system users, or from all users. Optionally delete the key from ServerPilot entirely."),
		mcp.WithArray("users",
			mcp.Description("System user IDs to remove the key from. If omitted, removes from all."),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithBoolean("delete_key",
			mcp.Description("Also delete the key from ServerPilot after removing from users"),
		),
	)
}

func resolveTargetUsers(d *Deps, userFilter []string) ([]spapi.SPSysUser, error) {
	if len(userFilter) == 0 {
		return d.SysUsers.List()
	}
	out := make([]spapi.SPSysUser, 0, len(userFilter))
	for _, id := range userFilter {
		u, err := d.SysUsers.Get(id)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, nil
}

func handleSSHSetup(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		userFilter := req.GetStringSlice("users", nil)
		var steps []string

		// Step 1: ensure key pair on disk.
		pair, err := mcsh.EnsureKeyPair(d.Cfg.SSHKeyPath, d.Cfg.SSHKeyName)
		if err != nil {
			return errResult(err)
		}
		steps = append(steps, fmt.Sprintf("SSH key pair at %s (exists or created)", d.Cfg.SSHKeyPath))

		// Step 2: ensure registered with ServerPilot.
		spKey, err := d.SSHKeys.FindByName(d.Cfg.SSHKeyName)
		if err != nil {
			return errResult(err)
		}
		if spKey != nil {
			steps = append(steps, fmt.Sprintf(`Key "%s" already registered with ServerPilot (ID: %s)`, d.Cfg.SSHKeyName, spKey.ID))
		} else {
			created, err := d.SSHKeys.Create(d.Cfg.SSHKeyName, pair.PublicKey)
			if err != nil {
				return errResult(err)
			}
			spKey = created
			steps = append(steps, fmt.Sprintf(`Key "%s" registered with ServerPilot (ID: %s)`, d.Cfg.SSHKeyName, spKey.ID))
		}

		// Step 3: assign to sysusers.
		targets, err := resolveTargetUsers(d, userFilter)
		if err != nil {
			return errResult(err)
		}
		var assigned, skipped, failed []string
		for _, u := range targets {
			userKeys, err := d.SSHKeys.ListForSysUser(u.ID)
			if err != nil {
				failed = append(failed, fmt.Sprintf("%s (%s) — %s", u.Name, u.ID, err.Error()))
				continue
			}
			already := false
			for _, k := range userKeys {
				if k.ID == spKey.ID {
					already = true
					break
				}
			}
			if already {
				skipped = append(skipped, fmt.Sprintf("%s (%s) — already has key", u.Name, u.ID))
				continue
			}
			if err := d.SSHKeys.AddToSysUser(spKey.ID, u.ID); err != nil {
				failed = append(failed, fmt.Sprintf("%s (%s) — %s", u.Name, u.ID, err.Error()))
				continue
			}
			assigned = append(assigned, fmt.Sprintf("%s (%s)", u.Name, u.ID))
		}
		if len(assigned) > 0 {
			steps = append(steps, "Assigned to: "+strings.Join(assigned, ", "))
		}
		if len(skipped) > 0 {
			steps = append(steps, "Skipped: "+strings.Join(skipped, ", "))
		}
		if len(failed) > 0 {
			steps = append(steps, "Failed: "+strings.Join(failed, ", "))
		}

		return mcp.NewToolResultText("SSH Bootstrap Complete\n\n" + strings.Join(steps, "\n")), nil
	}
}

type sshStatusUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ServerID string `json:"serverId"`
	HasKey   bool   `json:"hasKey"`
}

type sshStatus struct {
	KeyName                   string           `json:"keyName"`
	KeyPath                   string           `json:"keyPath"`
	LocalKeyExists            bool             `json:"localKeyExists"`
	RegisteredWithServerPilot bool             `json:"registeredWithServerPilot"`
	ServerPilotKeyID          *string          `json:"serverPilotKeyId"`
	Users                     []sshStatusUser  `json:"users,omitempty"`
}

func handleSSHStatus(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		spKey, err := d.SSHKeys.FindByName(d.Cfg.SSHKeyName)
		if err != nil {
			return errResult(err)
		}
		status := sshStatus{
			KeyName:                   d.Cfg.SSHKeyName,
			KeyPath:                   d.Cfg.SSHKeyPath,
			LocalKeyExists:            mcsh.KeyPairExists(d.Cfg.SSHKeyPath),
			RegisteredWithServerPilot: spKey != nil,
		}
		if spKey != nil {
			id := spKey.ID
			status.ServerPilotKeyID = &id
			users, err := d.SysUsers.List()
			if err != nil {
				return errResult(err)
			}
			out := make([]sshStatusUser, 0, len(users))
			for _, u := range users {
				userKeys, err := d.SSHKeys.ListForSysUser(u.ID)
				if err != nil {
					return errResult(err)
				}
				has := false
				for _, k := range userKeys {
					if k.ID == spKey.ID {
						has = true
						break
					}
				}
				out = append(out, sshStatusUser{
					ID: u.ID, Name: u.Name, ServerID: u.ServerID, HasKey: has,
				})
			}
			status.Users = out
		}
		return jsonText(status)
	}
}

func handleSSHRemove(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		userFilter := req.GetStringSlice("users", nil)
		deleteKey := req.GetBool("delete_key", false)

		spKey, err := d.SSHKeys.FindByName(d.Cfg.SSHKeyName)
		if err != nil {
			return errResult(err)
		}
		if spKey == nil {
			return mcp.NewToolResultText(
				fmt.Sprintf(`Key "%s" is not registered with ServerPilot. Nothing to remove.`, d.Cfg.SSHKeyName),
			), nil
		}

		targets, err := resolveTargetUsers(d, userFilter)
		if err != nil {
			return errResult(err)
		}
		var removed, notAssigned []string
		for _, u := range targets {
			userKeys, err := d.SSHKeys.ListForSysUser(u.ID)
			if err != nil {
				return errResult(err)
			}
			has := false
			for _, k := range userKeys {
				if k.ID == spKey.ID {
					has = true
					break
				}
			}
			if !has {
				notAssigned = append(notAssigned, fmt.Sprintf("%s (%s)", u.Name, u.ID))
				continue
			}
			if err := d.SSHKeys.RemoveFromSysUser(spKey.ID, u.ID); err != nil {
				return errResult(err)
			}
			removed = append(removed, fmt.Sprintf("%s (%s)", u.Name, u.ID))
		}

		var steps []string
		if len(removed) > 0 {
			steps = append(steps, "Removed from: "+strings.Join(removed, ", "))
		}
		if len(notAssigned) > 0 {
			steps = append(steps, "Not assigned: "+strings.Join(notAssigned, ", "))
		}
		if deleteKey {
			if err := d.SSHKeys.Delete(spKey.ID); err != nil {
				return errResult(err)
			}
			steps = append(steps, fmt.Sprintf(`Key "%s" deleted from ServerPilot`, d.Cfg.SSHKeyName))
		}
		return mcp.NewToolResultText("SSH Key Removal\n\n" + strings.Join(steps, "\n")), nil
	}
}
