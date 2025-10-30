package logger

import (
	"fmt"
    "io"
	"os"
	"path/filepath"
	"time"
    "strconv"
    "compress/gzip"

	"github.com/lestrrat-go/file-rotatelogs"
    log "github.com/sirupsen/logrus"
)

var IsDebug = true // Set this to true or false based on your debug preference

// LogFormatter log formatter structure
type LogFormatter struct {
	TimestampFormat string
	LevelDesc       []string
}

// Format format entry in custom format
func (f *LogFormatter) Format(entry *log.Entry) ([]byte, error) {
	timestamp := entry.Time.Format(f.TimestampFormat)
	level := f.LevelDesc[entry.Level]
	msg := fmt.Sprintf("%s [%s] %s\n", timestamp, level, entry.Message)
	return []byte(msg), nil
}

// createLogFolder creates a folder for logs based on the current date
func createLogFolder(logFile string) (string, error) {
	baseDir := filepath.Dir(logFile)
	now := time.Now()
	dateFolder := filepath.Join(baseDir, now.Format("2006-01-02"))
	err := os.MkdirAll(dateFolder, 0755)
	return dateFolder, err
}

// initializeLogRotation initializes log rotation with specified settings
func initializeLogRotation(logFile, dateFolder string, logFileMaxAge int) (*rotatelogs.RotateLogs, error) {
	return rotatelogs.New(
		// Specify the pattern for log file names, including the date and hour
		fmt.Sprintf("%s/%%Y-%%m-%%d-%%H%s", dateFolder, filepath.Base(logFile)),
		// Set the link name for the current log file
		rotatelogs.WithLinkName(fmt.Sprintf("%s/%s", dateFolder, filepath.Base(logFile))),
		// Set the log rotation time to one hour
		rotatelogs.WithRotationTime(time.Hour),
		// Set the maximum age for log files before they are deleted
		rotatelogs.WithMaxAge(time.Duration(logFileMaxAge)*24*time.Hour),
		// Set a handler to compress the previous log file after rotation
		rotatelogs.WithHandler(rotatelogs.HandlerFunc(func(e rotatelogs.Event) {
			// Check if the event is a file rotation event
			if e.Type() != rotatelogs.FileRotatedEventType {
				return
			}
			// Compress the previous log file after rotation
			compressPreviousFile(e.(*rotatelogs.FileRotatedEvent).PreviousFile())
		})),
	)
}

// OpenLog initializes and opens the log with rotation and cleanup settings
func OpenLog() {
	logFormatter := &LogFormatter{
		TimestampFormat: "2006-01-02 15:04:05.000",
		LevelDesc:       []string{"PANIC", "FATAL", "ERROR", "WARN", "INFO", "DEBUG", "TRACE"},
	}
	log.SetFormatter(logFormatter)

	// Set log level based on IsDebug flag
    if IsDebug {
        log.SetLevel(log.DebugLevel)
    } else {
        log.SetLevel(log.InfoLevel)
    }

	logDirectory := os.Getenv("LOG_DIRECTORY")
if logDirectory == "" {
	logDirectory = "./logs"
}


	logFileMaxAgeStr := os.Getenv("LOG_FILE_MAX_AGE")
	logFileMaxAge, err := strconv.Atoi(logFileMaxAgeStr)
	if err != nil || logFileMaxAge <= 0 {
		logFileMaxAge = 2 // Default max age in days
	}

	logFile := filepath.Join(logDirectory, ".log")
	dateFolder, err := createLogFolder(logFile)
	if err != nil {
		fmt.Println("Error creating folder:", err)
		os.Exit(1)
	}

	rl, rERROR := initializeLogRotation(logFile, dateFolder, logFileMaxAge)
	if rERROR != nil {
		fmt.Println(rERROR)
		os.Exit(1)
	}
	log.SetOutput(rl)

	// Start a routine to delete old log files
	deleteOldLogFilesRoutine(logDirectory, logFileMaxAge)
}

// WriteLog writes a log entry at the specified level with UUID and message
func WriteLog(level string, UUID string, key string, message interface{}) {
	if UUID == "" {
		UUID = "no-uuid-found"
	}

	message = fmt.Sprintf("[%v] [%v] | %+v", key, UUID, message)
	switch level {
	case "ERROR":
		log.Error(message)
	case "DEBUG":
		if IsDebug {
			log.Debug(message)
		}
	default:
		log.Info(message)
	}
}

// deleteOldLogFilesRoutine starts a routine to delete old log folders
func deleteOldLogFilesRoutine(logDirectory string, logFileMaxAge int) {
	go func() {
		for {
			// deleteOldLogFiles(logFile, logFileMaxAge)
			deleteOldDateFolders(logDirectory, logFileMaxAge)
			time.Sleep(time.Hour) // Check every hour
		}
	}()
}

// deleteOldDateFolders deletes date folders older than the specified max age
func deleteOldDateFolders(baseDir string, maxAgeDays int) {
	cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)
	fmt.Printf("Deleting folders older than: %v\n", cutoff)

	err := filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Handling directory access errors and continuing
			if os.IsNotExist(err) {
				fmt.Printf("Directory %s no longer exists: %v\n", path, err)
				return nil
			}
			fmt.Printf("Error accessing path %s: %v\n", path, err)
			return err
		}

		// Check if the directory is older than the cutoff date
		if d.IsDir() && path != baseDir {
			dirInfo, err := os.Stat(path)
			if err != nil {
				fmt.Printf("Failed to stat directory %s: %v\n", path, err)
				return err
			}
			// dirName := filepath.Base(path)
			// dirDate, _ := time.Parse("2006-01-02", dirName)
			// Debug output for modification time and comparison
			// fmt.Printf("Directory: %s, ModTime: %v\n", path, dirInfo.ModTime())
			// fmt.Printf("Cutoff: %v\n", cutoff)
			// if dirDate.Before(cutoff)

			// If the directory's modification time is before the cutoff date, delete it
			if dirInfo.ModTime().Before(cutoff) {
				fmt.Printf("Deleting old directory: %s\n", path)
				err := os.RemoveAll(path)
				if err != nil {
					fmt.Printf("Failed to delete directory %s: %v\n", path, err)
				} else {
					fmt.Printf("Successfully deleted directory: %s\n", path)
				}
			}
		}
		return nil
	})

	if err != nil {
		fmt.Printf("Failed to walk through directories: %v\n", err)
	}
}

// deleteOldLogFiles deletes log files older than the specified max age
func deleteOldLogFiles(logFileName string, logFileMaxAge int) {
	files, err := filepath.Glob(logFileName + ".*")
	if err != nil {
		log.Printf("Failed to list log files: %v", err)
		return
	}

	cutoff := time.Now().Add(-time.Duration(logFileMaxAge) * 24 * time.Hour)
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			log.Printf("Failed to load log file: %v", err)
			continue
		}

		if info.ModTime().Before(cutoff) {
			err := os.Remove(file)
			if err != nil {
				log.Printf("Failed to delete log file: %v", err)
			} else {
				log.Printf("Deleted old log file: %s", file)
			}
		}
	}
}

// compressPreviousFile compresses the previous log file
func compressPreviousFile(fileName string) {
	compressLogFile(fileName, fileName+".gz")
}

// compressLogFile compresses a log file to gzip format
func compressLogFile(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}
	defer f.Close()
	fi, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat log file: %v", err)
	}
	if err := chown(dst, fi); err != nil {
		return fmt.Errorf("failed to chown compressed log file: %v", err)
	}
	gzf, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fi.Mode())
	if err != nil {
		return fmt.Errorf("failed to open compressed log file: %v", err)
	}
	defer gzf.Close()
	gz := gzip.NewWriter(gzf)
	defer gz.Close()
	if _, err := io.Copy(gz, f); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		return err
	}
	return nil
}

// chown changes the owner of a file (stub function, no-op)
func chown(_ string, _ os.FileInfo) error {
	return nil
}