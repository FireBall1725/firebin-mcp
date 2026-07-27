// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

package tools

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/firelabsca/firebin-mcp/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ─── Wire shapes ─────────────────────────────────────────────────────────────

type apiProject struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Tags        []string   `json:"tags"`
	Boards      []apiBoard `json:"boards"`
	BoardCount  int        `json:"board_count"`
}

type apiBoard struct {
	ID             string `json:"id"`
	ProjectID      string `json:"project_id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Revision       string `json:"revision"`
	SourceFilename string `json:"source_filename"`
	Kind           string `json:"kind"`
	Copies         int    `json:"copies"`
	LineCount      int    `json:"line_count"`
}

type apiPickList struct {
	BoardID    string              `json:"board_id"`
	BoardName  string              `json:"board_name"`
	Quantity   int                 `json:"quantity"`
	Copies     int                 `json:"copies"`
	TotalUnits float64             `json:"total_units"`
	Entries    []apiPickEntry      `json:"entries"`
	Shortfalls []apiPickShortfall  `json:"shortfalls"`
	Unmatched  []apiPickUnmatched  `json:"unmatched"`
}

type apiPickEntry struct {
	StockItemID  string  `json:"stock_item_id"`
	PartID       string  `json:"part_id"`
	PartName     string  `json:"part_name"`
	LocationName string  `json:"location_name"`
	Quantity     float64 `json:"quantity"`
}

type apiPickShortfall struct {
	PartID    string  `json:"part_id"`
	PartName  string  `json:"part_name"`
	Required  float64 `json:"required"`
	Available float64 `json:"available"`
	Short     float64 `json:"short"`
}

type apiPickUnmatched struct {
	Refs     string `json:"refs"`
	Value    string `json:"value"`
	Quantity int    `json:"quantity"` // total needed across the whole build
}

// ─── Projections ─────────────────────────────────────────────────────────────

// Board is one PCB (or panel) inside a project. board_id is what pick_list
// takes.
type Board struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Revision    string `json:"revision,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Copies      int    `json:"copies,omitempty"`
	BOMLines    int    `json:"bom_lines,omitempty"`
	Description string `json:"description,omitempty"`
}

// Project is a hardware project holding one or more boards.
type Project struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	BoardCount  int      `json:"board_count"`
	Boards      []Board  `json:"boards,omitempty"`
}

func toBoards(in []apiBoard) []Board {
	out := make([]Board, 0, len(in))
	for _, b := range in {
		out = append(out, Board{
			ID:          b.ID,
			Name:        b.Name,
			Revision:    b.Revision,
			Kind:        b.Kind,
			Copies:      b.Copies,
			BOMLines:    b.LineCount,
			Description: b.Description,
		})
	}
	return out
}

func toProject(p apiProject) Project {
	return Project{
		ID:          p.ID,
		Name:        p.Name,
		Description: truncateText(p.Description, 240),
		Tags:        p.Tags,
		BoardCount:  p.BoardCount,
		Boards:      toBoards(p.Boards),
	}
}

// ─── list_projects ───────────────────────────────────────────────────────────

type listProjectsArgs struct{}

type listProjectsResult struct {
	Projects []Project `json:"projects"`
	Count    int       `json:"count"`
}

// AddListProjects wires the list_projects tool.
func AddListProjects(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_projects",
		Description: "List the hardware projects with how many boards each contains. Board ids are not included here; call get_project to get them, then pass one to pick_list.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listProjectsArgs) (*mcp.CallToolResult, listProjectsResult, error) {
		projects, err := api.Get[[]apiProject](ctx, client, "/projects")
		if err != nil {
			return nil, listProjectsResult{}, toolError(err)
		}
		out := make([]Project, 0, len(projects))
		for _, p := range projects {
			out = append(out, toProject(p))
		}
		return nil, listProjectsResult{Projects: out, Count: len(out)}, nil
	})
}

// ─── get_project ─────────────────────────────────────────────────────────────

type getProjectArgs struct {
	ID string `json:"id" jsonschema:"The project id (a UUID), as returned by list_projects."`
}

type getProjectResult struct {
	Project Project `json:"project"`
}

// AddGetProject wires the get_project tool.
func AddGetProject(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_project",
		Description: "Get one hardware project with all of its boards, their revisions, and how many bill-of-materials lines each board has.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getProjectArgs) (*mcp.CallToolResult, getProjectResult, error) {
		id := strings.TrimSpace(args.ID)
		if id == "" {
			return nil, getProjectResult{}, fmt.Errorf("id is required")
		}
		p, err := api.Get[apiProject](ctx, client, "/projects/"+url.PathEscape(id))
		if err != nil {
			return nil, getProjectResult{}, toolError(err)
		}
		return nil, getProjectResult{Project: toProject(p)}, nil
	})
}

// ─── pick_list ───────────────────────────────────────────────────────────────

type pickListArgs struct {
	BoardID  string `json:"board_id" jsonschema:"The board id (a UUID). Use list_projects or get_project to find it."`
	Quantity int    `json:"quantity,omitempty" jsonschema:"How many copies of the board to build. Defaults to 1."`
}

// PickEntry is one line of the pick list: pull this many of this part out of
// this bin.
type PickEntry struct {
	Part        string  `json:"part"`
	PartID      string  `json:"part_id"`
	StockItemID string  `json:"stock_item_id"`
	Location    string  `json:"location,omitempty"`
	Quantity    float64 `json:"quantity"`
}

// Shortfall is a part the build needs more of than the inventory holds.
type Shortfall struct {
	Part      string  `json:"part"`
	PartID    string  `json:"part_id"`
	Required  float64 `json:"required"`
	Available float64 `json:"available"`
	Short     float64 `json:"short"`
}

// UnmatchedLine is a BOM line that was never linked to an inventory part, so
// it cannot be picked or checked at all.
type UnmatchedLine struct {
	Refs     string `json:"refs,omitempty"`
	Value    string `json:"value,omitempty"`
	Quantity int    `json:"quantity,omitempty"`
}

type pickListResult struct {
	Board      string          `json:"board"`
	BoardID    string          `json:"board_id"`
	Quantity   int             `json:"quantity"`
	TotalUnits float64         `json:"total_units"`
	CanBuild   bool            `json:"can_build"`
	Entries    []PickEntry     `json:"entries"`
	Shortfalls []Shortfall     `json:"shortfalls,omitempty"`
	Unmatched  []UnmatchedLine `json:"unmatched,omitempty"`
	Note       string          `json:"note,omitempty"`
}

// AddPickList wires the pick_list tool.
func AddPickList(srv *mcp.Server, client *api.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "pick_list",
		Description: "Work out whether a board can be built and what to pull from which bin to build it. " +
			"Returns one pick line per part and bin, plus any parts there is not enough stock of, plus any bill-of-materials lines that were never matched to an inventory part. " +
			"Unmatched lines are not the same as shortfalls: they are unknown quantities, so a build with unmatched lines is not confirmed buildable even when nothing is short.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args pickListArgs) (*mcp.CallToolResult, pickListResult, error) {
		id := strings.TrimSpace(args.BoardID)
		if id == "" {
			return nil, pickListResult{}, fmt.Errorf("board_id is required")
		}
		qty := args.Quantity
		if qty <= 0 {
			qty = 1
		}

		pl, err := api.Get[apiPickList](ctx, client,
			"/boards/"+url.PathEscape(id)+"/pick-list?quantity="+strconv.Itoa(qty))
		if err != nil {
			return nil, pickListResult{}, toolError(err)
		}

		res := pickListResult{
			Board:      pl.BoardName,
			BoardID:    pl.BoardID,
			Quantity:   pl.Quantity,
			TotalUnits: pl.TotalUnits,
			CanBuild:   len(pl.Shortfalls) == 0 && len(pl.Unmatched) == 0,
			Entries:    make([]PickEntry, 0, len(pl.Entries)),
		}
		for _, e := range pl.Entries {
			res.Entries = append(res.Entries, PickEntry{
				Part: e.PartName, PartID: e.PartID, StockItemID: e.StockItemID,
				Location: e.LocationName, Quantity: e.Quantity,
			})
		}
		for _, s := range pl.Shortfalls {
			res.Shortfalls = append(res.Shortfalls, Shortfall{
				Part: s.PartName, PartID: s.PartID,
				Required: s.Required, Available: s.Available, Short: s.Short,
			})
		}
		for _, u := range pl.Unmatched {
			res.Unmatched = append(res.Unmatched, UnmatchedLine{
				Refs: u.Refs, Value: u.Value, Quantity: u.Quantity,
			})
		}

		switch {
		case len(pl.Shortfalls) > 0 && len(pl.Unmatched) > 0:
			res.Note = fmt.Sprintf("Short on %d part(s), and %d bill-of-materials line(s) are not matched to any inventory part.", len(pl.Shortfalls), len(pl.Unmatched))
		case len(pl.Shortfalls) > 0:
			res.Note = fmt.Sprintf("Short on %d part(s). Everything else is in stock.", len(pl.Shortfalls))
		case len(pl.Unmatched) > 0:
			res.Note = fmt.Sprintf("Every matched part is in stock, but %d bill-of-materials line(s) are not linked to an inventory part, so this is not a complete answer. Match them in the FireBin web UI.", len(pl.Unmatched))
		default:
			res.Note = "Everything needed is in stock."
		}
		return nil, res, nil
	})
}
