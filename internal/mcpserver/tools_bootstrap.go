package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/februality/serverpilot-mcp/internal/spapi"
	mcsh "github.com/februality/serverpilot-mcp/internal/ssh"
)

// RegisterBootstrapTools registers sp_ssh_setup, sp_ssh_status, sp_ssh_remove.
// sp_ssh_setup and sp_ssh_remove mutate ServerPilot key registrations and
// remote authorized_keys, so they are skipped when d.Cfg.ReadOnly is true.
func RegisterBootstrapTools(s *server.MCPServer, d *Deps) {
	s.AddTool(toolSSHStatus(), handleSSHStatus(d))
	if !d.Cfg.ReadOnly {
		s.AddTool(toolSSHSetup(), handleSSHSetup(d))
		s.AddTool(toolSSHRemove(), handleSSHRemove(d))
	}
}

func toolSSHSetup() mcp.Tool {
	return newTool("sp_ssh_setup", "Set up SSH access", kindAdditive, false,
		mcp.WithDescription("Enable the site_* tools: generate this machine's SSH key (if needed), register it with ServerPilot, and authorize it for system users. "+
			"Safe to re-run; users that already have the key are skipped. Pass site or users to limit the scope; with neither, the key is authorized for EVERY system user on every server."),
		mcp.WithString("site", mcp.Description("Authorize only the system user that owns this app (app name or domain)")),
		mcp.WithArray("users",
			mcp.Description("System user IDs or names to authorize. A name that exists on several servers (e.g. 'serverpilot') must be given as an ID."),
			mcp.Items(map[string]any{"type": "string"}),
		),
	)
}

func toolSSHStatus() mcp.Tool {
	return newTool("sp_ssh_status", "Check SSH access", kindRead, false,
		append([]mcp.ToolOption{
			mcp.WithDescription("Show whether this machine's SSH key exists locally and on ServerPilot, and which system users it is authorized for (i.e. which sites the site_* tools can reach). " +
				"Checking each user costs an API call, so pass site or server when you only care about one; the user list is paged (a note gives the next offset)."),
			mcp.WithString("site", mcp.Description("Only report the system user that owns this app (app name or domain)")),
			mcp.WithString("server", mcp.Description("Only report system users on this server (ID, name, or IP)")),
		}, pagingParams("system users", defaultPageLimit, sshStatusMaxLimit)...)...,
	)
}

func toolSSHRemove() mcp.Tool {
	return newTool("sp_ssh_remove", "Remove SSH access", kindIdempotent, false,
		mcp.WithDescription("Revoke this machine's SSH key from system users (site_* tools stop working for them). With no site or users, revokes it from all. Optionally delete the key from ServerPilot entirely."),
		mcp.WithString("site", mcp.Description("Revoke only from the system user that owns this app (app name or domain)")),
		mcp.WithArray("users",
			mcp.Description("System user IDs or names to revoke the key from"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithBoolean("delete_key",
			mcp.Description("Also delete the key from ServerPilot after removing it from users"),
		),
	)
}

// resolveTargetUsers turns the optional site / users arguments into system
// users. Only when BOTH are absent does it mean every system user on the
// account: a present-but-unusable users value (a bare string, a JSON-encoded
// string, an empty array, non-string items) is an error, never "all users",
// because for sp_ssh_setup / sp_ssh_remove that would silently widen the
// blast radius to every server.
func resolveTargetUsers(d *Deps, req mcp.CallToolRequest) ([]spapi.SPSysUser, error) {
	userFilter, usersGiven, err := userArg(req)
	if err != nil {
		return nil, err
	}
	site := req.GetString("site", "")
	if site == "" && !usersGiven {
		return d.SysUsers.List()
	}
	var out []spapi.SPSysUser
	seen := map[string]bool{}
	add := func(u *spapi.SPSysUser) {
		if !seen[u.ID] {
			seen[u.ID] = true
			out = append(out, *u)
		}
	}
	if site != "" {
		app, err := d.Apps.Resolve(site)
		if err != nil {
			return nil, err
		}
		u, err := d.SysUsers.Get(app.SysUserID)
		if err != nil {
			return nil, err
		}
		add(u)
	}
	for _, id := range userFilter {
		u, err := d.SysUsers.ResolveAny(id)
		if err != nil {
			return nil, err
		}
		add(u)
	}
	return out, nil
}

// userArg parses the users argument. It accepts a JSON array of strings, and
// tolerates the two common model mistakes of passing one name as a string or
// the array JSON-encoded inside a string.
func userArg(req mcp.CallToolRequest) (users []string, given bool, err error) {
	raw, ok := req.GetArguments()["users"]
	if !ok || raw == nil {
		return nil, false, nil
	}
	bad := fmt.Errorf("users must be a non-empty array of system user IDs or names, e.g. [\"alice\"]; omit it (and site) to target every system user")
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			str, ok := item.(string)
			if !ok || strings.TrimSpace(str) == "" {
				return nil, true, bad
			}
			users = append(users, str)
		}
	case []string:
		users = v
	case string:
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "[") {
			if err := json.Unmarshal([]byte(v), &users); err != nil {
				return nil, true, bad
			}
		} else if v != "" {
			users = []string{v}
		}
	default:
		return nil, true, bad
	}
	if len(users) == 0 {
		return nil, true, bad
	}
	return users, true, nil
}

// keyAssignments reports, for each user, whether keyID is authorized. The
// API has no bulk endpoint, so it fans out with bounded concurrency.
func keyAssignments(d *Deps, users []spapi.SPSysUser, keyID string) ([]bool, []error) {
	has := make([]bool, len(users))
	errs := make([]error, len(users))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, u := range users {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, id string) {
			defer wg.Done()
			defer func() { <-sem }()
			keys, err := d.SSHKeys.ListForSysUser(id)
			if err != nil {
				errs[i] = err
				return
			}
			for _, k := range keys {
				if k.ID == keyID {
					has[i] = true
					return
				}
			}
		}(i, u.ID)
	}
	wg.Wait()
	return has, errs
}

func handleSSHSetup(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		targets, err := resolveTargetUsers(d, req)
		if err != nil {
			return errResult(err)
		}
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
		has, lookupErrs := keyAssignments(d, targets, spKey.ID)
		var assigned, skipped, failed []string
		for i, u := range targets {
			if lookupErrs[i] != nil {
				failed = append(failed, fmt.Sprintf("%s (%s) — %s", u.Name, u.ID, lookupErrs[i].Error()))
				continue
			}
			if has[i] {
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

		if len(failed) == 0 {
			if len(assigned) > 0 {
				steps = append(steps, "site_* tools can now reach these users' apps. A newly authorized key can take a short time to reach the server; if the first SSH call is refused, wait a moment and retry.")
			}
			return mcp.NewToolResultText("SSH Bootstrap Complete\n\n" + strings.Join(steps, "\n")), nil
		}
		text := "SSH Bootstrap Incomplete\n\n" + strings.Join(steps, "\n")
		if len(assigned) == 0 && len(skipped) == 0 {
			return mcp.NewToolResultError(text), nil
		}
		return mcp.NewToolResultText(text), nil
	}
}

type sshStatusUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ServerID string `json:"serverId"`
	HasKey   bool   `json:"hasKey"`
}

type sshStatus struct {
	KeyName                   string          `json:"keyName"`
	KeyPath                   string          `json:"keyPath"`
	LocalKeyExists            bool            `json:"localKeyExists"`
	RegisteredWithServerPilot bool            `json:"registeredWithServerPilot"`
	ServerPilotKeyID          *string         `json:"serverPilotKeyId"`
	Users                     []sshStatusUser `json:"users,omitempty"`
}

// sshStatusMaxLimit keeps one sp_ssh_status page (~90 bytes per user) well
// under the result-size budget and bounds the per-user API calls per call.
const sshStatusMaxLimit = 300

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
			var users []spapi.SPSysUser
			if site := req.GetString("site", ""); site != "" {
				app, err := d.Apps.Resolve(site)
				if err != nil {
					return errResult(err)
				}
				u, err := d.SysUsers.Get(app.SysUserID)
				if err != nil {
					return errResult(err)
				}
				users = []spapi.SPSysUser{*u}
				if filter := req.GetString("server", ""); filter != "" { // both given: apply both
					srv, err := d.Servers.Resolve(filter)
					if err != nil {
						return errResult(err)
					}
					users = slices.DeleteFunc(users, func(x spapi.SPSysUser) bool { return x.ServerID != srv.ID })
				}
			} else if filter := req.GetString("server", ""); filter != "" {
				srv, err := d.Servers.Resolve(filter)
				if err != nil {
					return errResult(err)
				}
				users, err = d.SysUsers.ListByServer(srv.ID)
				if err != nil {
					return errResult(err)
				}
			} else {
				users, err = d.SysUsers.List()
				if err != nil {
					return errResult(err)
				}
			}
			// Page before the per-user lookups: each costs an API call.
			total := len(users)
			offset, limit := pageArgs(req, defaultPageLimit)
			limit = min(limit, sshStatusMaxLimit)
			if offset > 0 && offset >= total {
				return errResult(fmt.Errorf("offset %d is past the end of the list (%d system users)", offset, total))
			}
			users = users[offset:min(offset+limit, total)]

			has, errs := keyAssignments(d, users, spKey.ID)
			out := make([]sshStatusUser, 0, len(users))
			for i, u := range users {
				if errs[i] != nil {
					return errResult(errs[i])
				}
				out = append(out, sshStatusUser{
					ID: u.ID, Name: u.Name, ServerID: u.ServerID, HasKey: has[i],
				})
			}
			status.Users = out
			res, err := jsonText(status)
			if err == nil {
				if note := pageNote("system users", offset, len(out), total, "narrow with site or server"); note != "" {
					res.Content = append(res.Content, mcp.NewTextContent(note))
				}
			}
			return res, err
		}
		return jsonText(status)
	}
}

func handleSSHRemove(d *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		targets, err := resolveTargetUsers(d, req)
		if err != nil {
			return errResult(err)
		}
		has, lookupErrs := keyAssignments(d, targets, spKey.ID)
		var removed, notAssigned, failed []string
		for i, u := range targets {
			if lookupErrs[i] != nil {
				failed = append(failed, fmt.Sprintf("%s (%s) — %s", u.Name, u.ID, lookupErrs[i].Error()))
				continue
			}
			if !has[i] {
				notAssigned = append(notAssigned, fmt.Sprintf("%s (%s)", u.Name, u.ID))
				continue
			}
			if err := d.SSHKeys.RemoveFromSysUser(spKey.ID, u.ID); err != nil {
				failed = append(failed, fmt.Sprintf("%s (%s) — %s", u.Name, u.ID, err.Error()))
				continue
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
		if len(failed) > 0 {
			steps = append(steps, "Failed: "+strings.Join(failed, ", "))
		}
		if deleteKey {
			if err := d.SSHKeys.Delete(spKey.ID); err != nil {
				return errResult(err)
			}
			steps = append(steps, fmt.Sprintf(`Key "%s" deleted from ServerPilot`, d.Cfg.SSHKeyName))
		}
		text := "SSH Key Removal\n\n" + strings.Join(steps, "\n")
		if len(failed) > 0 && len(removed) == 0 && len(notAssigned) == 0 && !deleteKey {
			return mcp.NewToolResultError(text), nil
		}
		return mcp.NewToolResultText(text), nil
	}
}
