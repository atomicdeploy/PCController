//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"

	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/releaseplane"
	"pccontroller.local/desktop/desktop"
)

func (p nativePages) loadNativeReleases() desktop.Page {
	return desktop.Page{Title: "Release & workflow discovery", Columns: []string{"ID", "Name", "Kind", "Platform", "Build", "SHA-256"}, RowValues: map[string]map[string]string{}, Fields: []desktop.Field{
		field("repository", "GitHub repository", "atomicdeploy/PCController"), field("branch", "Branch", "main"), field("workflow", "Workflow", "build.yml"), field("tag", "Release tag (empty = latest)", ""), choice("kind", "Artifact kind", "firmware", "firmware", "eeprom", "host-executable"), field("platform", "Platform (optional)", ""), field("manifest_url", "Manifest URL", ""),
		{Key: "candidate", Hidden: true}, {Key: "candidates", Hidden: true}, field("operation_id", "Staging operation ID", ""),
	}, Actions: []desktop.PageAction{
		{ID: "workflow", Label: "Find workflow artifacts", Enabled: !p.preview, TimeoutSeconds: 60}, {ID: "release", Label: "Find release assets", Enabled: !p.preview, TimeoutSeconds: 60}, {ID: "manifest", Label: "Read remote manifest", Enabled: !p.preview, TimeoutSeconds: 60},
		{ID: "stage", Label: "Stage selected (no apply)", Enabled: !p.preview, SelectionRequired: true, TimeoutSeconds: 30}, {ID: "status", Label: "Staging progress", Enabled: !p.preview},
	}}
}

func nativeReleaseRows(page *desktop.Page, candidates []releaseplane.Candidate) {
	for _, item := range candidates {
		encoded, _ := json.Marshal(item)
		page.Rows = append(page.Rows, []string{item.ID, item.Name, string(item.Kind), item.Platform, item.BuildHash, item.SHA256})
		page.RowValues[item.ID] = map[string]string{"candidate": string(encoded)}
	}
	encoded, _ := json.Marshal(candidates)
	for i := range page.Fields {
		if page.Fields[i].Key == "candidates" {
			page.Fields[i].Value = string(encoded)
		}
	}
}

func (p nativePages) submitNativeReleases(ctx context.Context, action string, v map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot discover or stage updates")
	}
	page := p.loadNativeReleases()
	for i := range page.Fields {
		if value, ok := v[page.Fields[i].Key]; ok {
			page.Fields[i].Value = value
		}
	}
	var discovery releaseplane.DiscoveryResult
	var result any
	kind, err := artifacts.ParseKind(v["kind"])
	if err != nil {
		return page, err
	}
	platform := v["platform"]
	if platform == "" && kind == artifacts.KindHostExecutable {
		platform = runtime.GOOS + "/" + runtime.GOARCH
	}
	switch action {
	case "workflow":
		err = p.call(ctx, "controller.discovery.github.workflow", releaseplane.GitHubWorkflowRequest{Repository: v["repository"], Branch: v["branch"], Workflow: v["workflow"], Kind: kind, Platform: platform}, &discovery)
	case "release":
		err = p.call(ctx, "controller.discovery.github.release", releaseplane.GitHubReleaseRequest{Repository: v["repository"], Tag: v["tag"], Kind: kind, Platform: platform}, &discovery)
	case "manifest":
		err = p.call(ctx, "controller.discovery.manifest", releaseplane.ManifestRequest{URL: v["manifest_url"]}, &discovery)
	case "stage":
		var candidate releaseplane.Candidate
		var known []releaseplane.Candidate
		if json.Unmarshal([]byte(v["candidate"]), &candidate) != nil || candidate.ID != v["selected"] || json.Unmarshal([]byte(v["candidates"]), &known) != nil {
			return page, errors.New("select a discovered candidate first")
		}
		found := false
		for _, item := range known {
			if nativeDigest(item) == nativeDigest(candidate) {
				found = true
				break
			}
		}
		if !found {
			return page, errors.New("candidate metadata changed; rediscover before staging")
		}
		var staged releaseplane.StageResult
		err = p.call(ctx, "controller.discovery.stage", releaseplane.StageRequest{Candidate: candidate}, &staged)
		result = staged
		if err == nil {
			for i := range page.Fields {
				if page.Fields[i].Key == "operation_id" {
					page.Fields[i].Value = staged.Operation.ID
				}
			}
		}
	case "status":
		err = p.call(ctx, "controller.discovery.status", map[string]string{"id": v["operation_id"]}, &result)
	default:
		return page, errors.New("unknown release operation")
	}
	if err != nil {
		return page, err
	}
	if action == "workflow" || action == "release" || action == "manifest" {
		nativeReleaseRows(&page, discovery.Candidates)
		result = discovery
	} else {
		var candidates []releaseplane.Candidate
		if json.Unmarshal([]byte(v["candidates"]), &candidates) == nil {
			nativeReleaseRows(&page, candidates)
		}
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return page, fmt.Errorf("operation acknowledged; format result: %w", err)
	}
	page.Output = string(encoded)
	return page, nil
}
