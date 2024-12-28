package app

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/bradsec/gofindmyfonts/internal/logging"
)

type CleanupManager struct {
	config *Config
}

func NewCleanupManager(config *Config) *CleanupManager {
	return &CleanupManager{
		config: config,
	}
}

func (cm *CleanupManager) CleanOldFiles() error {
	convertedDir := filepath.Join(cm.config.StaticDir, "converted")
	logging.Info("Starting cleanup of old files", "cleanup", convertedDir)

	entries, err := os.ReadDir(convertedDir)
	if err != nil {
		logging.Error("Failed to read converted directory", "cleanup", convertedDir, err)
		return err
	}

	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		path := filepath.Join(convertedDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			logging.Error("Failed to get file info", "cleanup", path, err)
			continue
		}

		// Remove files older than cache time
		if now.Sub(info.ModTime()) > cm.config.PreviewCacheTime {
			if err := os.Remove(path); err != nil {
				logging.Error("Failed to remove old file", "cleanup", path, err)
			} else {
				logging.Info("Removed old file", "cleanup", path)
			}
		}
	}

	return nil
}

func (cm *CleanupManager) ScheduleCleanup(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				if err := cm.CleanOldFiles(); err != nil {
					logging.Error("Scheduled cleanup failed", "cleanup", "", err)
				}
			}
		}
	}()
}
