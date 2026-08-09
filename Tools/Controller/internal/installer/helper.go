package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pccontroller.local/controller/internal/pathguard"
	"pccontroller.local/controller/internal/productidentity"
)

const (
	uninstallHelperCommand = "__installation-uninstall-helper"
	uninstallPlanFormat    = "pccontroller-uninstall-helper/v1"
	uninstallOutcomeFormat = "pccontroller-uninstall-outcome/v1"
)

type uninstallHelperPlan struct {
	Format         string           `json:"format"`
	CreatedAt      time.Time        `json:"created_at"`
	ParentPID      int              `json:"parent_pid"`
	ParentIdentity string           `json:"parent_identity"`
	OwnerID        string           `json:"owner_id"`
	Platform       string           `json:"platform"`
	Architecture   string           `json:"architecture"`
	HelperPath     string           `json:"helper_path"`
	HelperSHA256   string           `json:"helper_sha256"`
	PlanPath       string           `json:"plan_path"`
	OutcomePath    string           `json:"outcome_path"`
	Request        UninstallRequest `json:"request"`
}

type ExternalUninstallPlan struct {
	HelperPath  string `json:"helper_path"`
	PlanPath    string `json:"plan_path"`
	OutcomePath string `json:"outcome_path"`
}

type uninstallOutcome struct {
	Format        string    `json:"format"`
	CompletedAt   time.Time `json:"completed_at"`
	Success       bool      `json:"success"`
	Error         string    `json:"error,omitempty"`
	Root          string    `json:"root"`
	DataPreserved bool      `json:"data_preserved"`
}

func IsUninstallHelperInvocation(args []string) bool {
	return len(args) == 2 && args[0] == uninstallHelperCommand && strings.TrimSpace(args[1]) != ""
}

// PrepareExternalUninstall copies the verified/hash-bound running host outside
// its installation root and starts a native helper. The helper waits for this
// process to exit before detaching the installation directory.
func (service *Service) PrepareExternalUninstall(ctx context.Context, request UninstallRequest) (ExternalUninstallPlan, error) {
	if err := service.validate(); err != nil {
		return ExternalUninstallPlan{}, err
	}
	root, err := safeInstallRoot(request.Root)
	if err != nil {
		return ExternalUninstallPlan{}, err
	}
	if !pathWithin(root, service.CurrentExecutable) {
		return ExternalUninstallPlan{}, errors.New("external uninstall helper is only needed while running from the installation root")
	}
	status, err := service.Status(ctx, root)
	if err != nil {
		return ExternalUninstallPlan{}, err
	}
	if !status.Healthy {
		return ExternalUninstallPlan{}, errors.New("refusing to clone a damaged installed executable as the uninstall helper")
	}
	trustedCurrent := samePath(service.CurrentExecutable, status.Executable)
	if !trustedCurrent && status.State != nil && status.State.PreviousSlot != "" {
		if err := service.verifySlot(root, status.State.PreviousSlot, status.State.PreviousSHA256); err == nil {
			activePrefix := strings.TrimSuffix(filepath.ToSlash(status.State.ActiveSlot), "/") + "/"
			relativeExecutable := strings.TrimPrefix(
				filepath.ToSlash(status.State.Executable), activePrefix,
			)
			if relativeExecutable != filepath.ToSlash(status.State.Executable) {
				previousExecutable := filepath.Join(
					root, filepath.FromSlash(status.State.PreviousSlot),
					filepath.FromSlash(relativeExecutable),
				)
				trustedCurrent = samePath(service.CurrentExecutable, previousExecutable)
			}
		}
	}
	if !trustedCurrent {
		return ExternalUninstallPlan{}, errors.New("running executable is not an inventoried active or rollback package")
	}
	if request.PurgeData {
		if request.PurgeConfirmation != PurgeConfirmation {
			return ExternalUninstallPlan{}, fmt.Errorf("data purge requires the exact separate confirmation %q", PurgeConfirmation)
		}
		if _, err := preparePurgeTargets(request, root, service.OwnerID); err != nil {
			return ExternalUninstallPlan{}, err
		}
	}
	id, err := transactionID()
	if err != nil {
		return ExternalUninstallPlan{}, err
	}
	directory := filepath.Join(os.TempDir(), productidentity.ConfigDirectory+"-uninstall-"+id)
	if err := pathguard.ValidateComponents(directory, true); err != nil {
		return ExternalUninstallPlan{}, err
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		return ExternalUninstallPlan{}, err
	}
	if err := pathguard.ValidateComponents(directory, false); err != nil {
		return ExternalUninstallPlan{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = removeTreeSecure(directory)
		}
	}()
	extension := filepath.Ext(service.CurrentExecutable)
	helperPath := filepath.Join(directory, "uninstall-helper"+extension)
	digest, bytes, err := digestFile(service.CurrentExecutable)
	if err != nil {
		return ExternalUninstallPlan{}, err
	}
	if err := copyVerifiedFile(service.CurrentExecutable, helperPath, PackageFile{
		Path: filepath.Base(helperPath), Bytes: bytes, SHA256: digest,
	}); err != nil {
		return ExternalUninstallPlan{}, err
	}
	planPath := filepath.Join(directory, "uninstall-plan.json")
	outcomePath := filepath.Join(directory, "uninstall-outcome.json")
	parentIdentity, err := parentProcessIdentity(os.Getpid())
	if err != nil {
		return ExternalUninstallPlan{}, fmt.Errorf("bind uninstall helper to parent identity: %w", err)
	}
	plan := uninstallHelperPlan{
		Format: uninstallPlanFormat, CreatedAt: service.now(), ParentPID: os.Getpid(),
		ParentIdentity: parentIdentity,
		OwnerID:        service.OwnerID, Platform: service.Platform, Architecture: service.Architecture,
		HelperPath: helperPath, HelperSHA256: digest, PlanPath: planPath,
		OutcomePath: outcomePath, Request: request,
	}
	if err := writeJSONAtomic(planPath, plan, 0o600); err != nil {
		return ExternalUninstallPlan{}, err
	}
	if err := launchUninstallHelper(ctx, helperPath, planPath); err != nil {
		return ExternalUninstallPlan{}, err
	}
	cleanup = false
	return ExternalUninstallPlan{HelperPath: helperPath, PlanPath: planPath, OutcomePath: outcomePath}, nil
}

func RunExternalUninstallHelper(ctx context.Context, planPath string, service *Service) (resultErr error) {
	if service == nil {
		return errors.New("uninstall helper requires an installation service")
	}
	content, err := readBoundedRegularFile(planPath, 256<<10)
	if err != nil {
		return err
	}
	var plan uninstallHelperPlan
	if err := decodeStrictJSON(content, &plan); err != nil {
		return err
	}
	if err := service.validate(); err != nil {
		return err
	}
	now := service.now()
	if plan.Format != uninstallPlanFormat || plan.ParentPID <= 0 || strings.TrimSpace(plan.ParentIdentity) == "" || plan.OwnerID != service.OwnerID || plan.Platform != service.Platform || plan.Architecture != service.Architecture || !samePath(plan.PlanPath, planPath) || now.Before(plan.CreatedAt.Add(-time.Minute)) || now.After(plan.CreatedAt.Add(15*time.Minute)) {
		return errors.New("uninstall helper plan identity or lifetime is invalid")
	}
	planDirectory := filepath.Dir(planPath)
	if filepath.Base(plan.OutcomePath) != "uninstall-outcome.json" || !samePath(filepath.Dir(plan.OutcomePath), planDirectory) || !samePath(filepath.Dir(plan.HelperPath), planDirectory) {
		return errors.New("uninstall helper plan paths are not co-located")
	}
	if err := pathguard.ValidateComponents(plan.OutcomePath, true); err != nil {
		return err
	}
	current, err := filepath.Abs(service.CurrentExecutable)
	if err != nil || !samePath(current, plan.HelperPath) {
		return errors.New("uninstall helper executable does not match its plan")
	}
	digest, _, err := digestFile(current)
	if err != nil || !strings.EqualFold(digest, plan.HelperSHA256) {
		return errors.New("uninstall helper executable digest differs from its plan")
	}
	if err := waitForParentExit(ctx, plan.ParentPID, plan.ParentIdentity, 3*time.Minute); err != nil {
		resultErr = err
	} else if _, err := service.Uninstall(ctx, plan.Request); err != nil {
		resultErr = err
	}
	outcome := uninstallOutcome{
		Format: uninstallOutcomeFormat, CompletedAt: service.now(), Success: resultErr == nil,
		Root: plan.Request.Root, DataPreserved: !plan.Request.PurgeData,
	}
	if resultErr != nil {
		outcome.Error = resultErr.Error()
	}
	if err := writeJSONAtomic(plan.OutcomePath, outcome, 0o600); err != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("persist uninstall helper outcome: %w", err))
	}
	if resultErr == nil {
		_ = os.Remove(plan.PlanPath)
		_ = scheduleHelperRemoval(plan.HelperPath)
	}
	return resultErr
}
