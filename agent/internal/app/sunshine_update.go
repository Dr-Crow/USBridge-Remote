package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"usbridge_agent/internal/forkrelease"
	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/netpolicy"
	"usbridge_agent/internal/streamhost"
)

// Sunshine from Streamers-Forks (forkrelease.PrepareSunshine) where the agent
// no longer ships it (Windows, Linux x86_64, Apple Silicon): downloaded the
// first time Sunshine is the streamer -- on a fresh install that is the first
// start -- and updated from then on, like Punktfunk.

// sunshineUpdateInterval: as punktfunkUpdateInterval.
const sunshineUpdateInterval = 6 * time.Hour

// sunshineFetchRetry spaces out first-run download attempts that failed
// (offline, GitHub down); sunshineWatchdog asks every 15s.
const sunshineFetchRetry = time.Minute

// sunshineBusyRetry: an update found while a client is streaming is tried
// again this much later instead of cutting the stream.
const sunshineBusyRetry = 15 * time.Minute

var errSunshineStreaming = errors.New("a client is streaming")

// sunshineDownloadable: this platform's Sunshine comes from Streamers-Forks.
func sunshineDownloadable() bool { return forkrelease.SunshineAssetName() != "" }

// sunshineMissing: Sunshine is downloaded here and there is none yet (no
// download, and no bundled one from an older agent install).
func (a *App) sunshineMissing() bool {
	if netpolicy.Strict() {
		return localcomponents.PreparedPath(a.cfg.StateDir, "sunshine") == ""
	}
	if !sunshineDownloadable() || forkrelease.SunshineStaged(a.cfg.StateDir) {
		return false
	}
	if a.stream != nil && a.currentStreamKind() == "sunshine" {
		if p := a.stream.BinaryPath(); p != "" {
			if _, err := os.Stat(p); err == nil {
				return false
			}
		}
	}
	return true
}

// sunshineOnDisk: there is a Sunshine to start, downloaded or bundled.
func (a *App) sunshineOnDisk() bool {
	if netpolicy.Strict() {
		return localcomponents.PreparedPath(a.cfg.StateDir, "sunshine") != ""
	}
	if forkrelease.SunshineStaged(a.cfg.StateDir) {
		return true
	}
	p := streamhost.NewSunshine(a.exeDir, a.cfg.StateDir, "").BinaryPath()
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// fetchSunshineInBackground starts the first-run download unless one is
// running or the last one failed too recently, and starts Sunshine once it
// is there. Called by startSunshineNow instead of starting a Sunshine that
// isn't on disk.
func (a *App) fetchSunshineInBackground() {
	a.entMu.Lock()
	busy := a.entStatus.SunshineUpdateInProgress || time.Since(a.lastSunshineFetch) < sunshineFetchRetry
	if !busy {
		a.lastSunshineFetch = time.Now()
		a.entStatus.SunshineUpdateInProgress = true
	}
	a.entMu.Unlock()
	if busy {
		return
	}
	go func() {
		err := a.downloadSunshine(nil)
		a.endSunshineUpdate()
		if err != nil {
			return
		}
		if a.currentStreamKind() == "sunshine" {
			a.startSunshine()
		}
	}()
}

// DownloadSunshine downloads Sunshine (signature and SHA-256 checked) and
// puts it in place; SetStreamBackend("sunshine") calls it when there is
// none. Progress goes through entStatus like DownloadPunktfunk's.
func (a *App) DownloadSunshine(onProgress forkrelease.ProgressFunc) error {
	if netpolicy.Strict() {
		return a.prepareLocalComponent(context.Background(), "sunshine")
	}
	a.entMu.Lock()
	if a.entStatus.SunshineUpdateInProgress {
		a.entMu.Unlock()
		return fmt.Errorf("sunshine download already in progress")
	}
	a.entStatus.SunshineUpdateInProgress = true
	a.entMu.Unlock()
	defer a.endSunshineUpdate()
	return a.downloadSunshine(onProgress)
}

func (a *App) downloadSunshine(onProgress forkrelease.ProgressFunc) error {
	if netpolicy.Strict() {
		return a.prepareLocalComponent(context.Background(), "sunshine")
	}
	a.entMu.Lock()
	a.entStatus.DownloadInProgress = true
	a.entStatus.DownloadName = "Sunshine"
	a.entStatus.Progress = -1
	a.entStatus.LastError = ""
	a.entMu.Unlock()
	defer func() {
		a.entMu.Lock()
		a.entStatus.DownloadInProgress = false
		a.entStatus.DownloadName = ""
		a.entMu.Unlock()
	}()
	combined := func(downloaded, total int64) {
		frac := -1.0
		if total > 0 {
			frac = float64(downloaded) / float64(total)
		}
		a.entMu.Lock()
		a.entStatus.Progress = frac
		a.entMu.Unlock()
		if onProgress != nil {
			onProgress(downloaded, total)
		}
	}
	log.Printf("[app] downloading Sunshine (Streamers-Forks latest release)")
	prep, err := forkrelease.PrepareSunshine(context.Background(), a.cfg.StateDir, combined)
	if err == nil {
		if err = prep.Commit(); err != nil {
			prep.Discard()
		}
	}
	if err != nil {
		a.setEntError(fmt.Sprintf("sunshine download failed: %v", err))
		log.Printf("[app] sunshine download failed: %v", err)
		return fmt.Errorf("sunshine download failed: %w", err)
	}
	log.Printf("[app] Sunshine %s downloaded and verified", prep.Version)
	return nil
}

func (a *App) endSunshineUpdate() {
	a.entMu.Lock()
	a.entStatus.SunshineUpdateInProgress = false
	a.entMu.Unlock()
}

// checkSunshineUpdate installs a newer Sunshine from Streamers-Forks every
// sunshineUpdateInterval -- only one the agent downloaded itself; a bundled
// one from an older install moves over through the card's button. Never cuts
// a stream: with a client connected it waits for sunshineBusyRetry.
func (a *App) checkSunshineUpdate(ctx context.Context) {
	if netpolicy.RuntimeLocal() {
		return
	}
	if !sunshineDownloadable() || !forkrelease.SunshineStaged(a.cfg.StateDir) {
		return
	}
	a.entMu.Lock()
	due := time.Since(a.lastSunshineCheck) >= sunshineUpdateInterval && !a.entStatus.SunshineUpdateInProgress
	if due {
		a.lastSunshineCheck = time.Now()
		a.entStatus.SunshineUpdateInProgress = true
	}
	a.entMu.Unlock()
	if !due {
		return
	}
	defer a.endSunshineUpdate()
	err := a.updateSunshine(ctx, true)
	if errors.Is(err, errSunshineStreaming) {
		a.entMu.Lock()
		a.lastSunshineCheck = time.Now().Add(sunshineBusyRetry - sunshineUpdateInterval)
		a.entMu.Unlock()
		log.Printf("[app] sunshine update postponed: %v", err)
	} else if err != nil {
		log.Printf("[app] sunshine update failed (will retry): %v", err)
	}
}

// CheckSunshineUpdateNow is the Sunshine card's "check for updates": the
// latest Streamers-Forks release, right now, applied even mid-stream (the
// user asked). It also replaces a Sunshine bundled with an older agent.
func (a *App) CheckSunshineUpdateNow() error {
	if err := netpolicy.RequireOnline("public Sunshine update"); err != nil {
		return err
	}
	if !sunshineDownloadable() {
		return fmt.Errorf("sunshine is updated together with the agent on this platform")
	}
	a.entMu.Lock()
	if a.entStatus.SunshineUpdateInProgress {
		a.entMu.Unlock()
		return fmt.Errorf("sunshine update already in progress")
	}
	a.entStatus.SunshineUpdateInProgress = true
	a.entStatus.LastError = ""
	a.lastSunshineCheck = time.Now()
	a.entMu.Unlock()
	defer a.endSunshineUpdate()
	if err := a.updateSunshine(context.Background(), false); err != nil {
		a.setEntError(fmt.Sprintf("sunshine update failed: %v", err))
		return err
	}
	return nil
}

// updateSunshine installs the latest release's Sunshine when it differs from
// the downloaded one (or there is no downloaded one yet). A Sunshine that is
// the active streamer is stopped for the swap -- Windows can't replace a
// running .exe -- and started again from the new tree. On Linux with KMS
// capture the launcher keeps running the root-owned copy of the previous
// tree until screen capture is granted again (permissions.installSunshineKMS).
func (a *App) updateSunshine(ctx context.Context, spareStream bool) error {
	staged := forkrelease.SunshineStaged(a.cfg.StateDir)
	latest, err := forkrelease.LatestSunshineVersion(ctx)
	if err != nil {
		return err
	}
	if latest == "" || (staged && latest == forkrelease.SunshineStagedVersion(a.cfg.StateDir)) {
		return nil
	}
	if spareStream && a.currentStreamKind() == "sunshine" && a.SessionActive() {
		return errSunshineStreaming
	}
	log.Printf("[app] Sunshine %s available -- downloading", latest)
	prep, err := forkrelease.PrepareSunshine(ctx, a.cfg.StateDir, nil)
	if err != nil {
		return err
	}
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	active := a.streamKind == "sunshine" && a.stream != nil
	if active && spareStream && a.stream.SessionActive() {
		prep.Discard()
		return errSunshineStreaming
	}
	if active {
		a.stopStreamAndWait(a.stream)
	}
	commitErr := prep.Commit()
	if commitErr != nil {
		prep.Discard()
		commitErr = fmt.Errorf("sunshine update not applied: %w", commitErr)
	} else {
		log.Printf("[app] Sunshine updated to %s", prep.Version)
	}
	if active {
		if err := a.RestartSunshineStartOnly(); err != nil {
			log.Printf("[app] sunshine restart after update failed: %v", err)
		}
	}
	return commitErr
}
