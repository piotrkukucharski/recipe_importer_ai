package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	"cloud.google.com/go/storage"
	"golang.org/x/oauth2"
	"google.golang.org/api/option"
)

type TranscriptionService struct {
	SpeechClient  *speech.Client
	StorageClient *storage.Client
	BucketName    string
}

func NewTranscriptionService(ctx context.Context) (*TranscriptionService, error) {
	var options []option.ClientOption

	// Support for raw access token instead of JSON key file
	if token := os.Getenv("GOOGLE_ACCESS_TOKEN"); token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken: token,
		})
		options = append(options, option.WithTokenSource(ts))
	}

	speechClient, err := speech.NewClient(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create speech client: %v", err)
	}

	storageClient, err := storage.NewClient(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %v", err)
	}

	bucketName := os.Getenv("GOOGLE_CLOUD_STORAGE_BUCKET")
	if bucketName == "" {
		return nil, fmt.Errorf("GOOGLE_CLOUD_STORAGE_BUCKET environment variable is not set")
	}

	return &TranscriptionService{
		SpeechClient:  speechClient,
		StorageClient: storageClient,
		BucketName:    bucketName,
	}, nil
}

func (s *TranscriptionService) TranscribeVideo(ctx context.Context, videoURL string, correlationID string) (string, error) {
	LogJSON(correlationID, "Transcription", fmt.Sprintf("Starting transcription process for video: %s", videoURL), "INFO")

	// 1. Create temporary directory
	tmpDir, err := os.MkdirTemp("", "transcription-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "video.mp4")
	audioPath := filepath.Join(tmpDir, "audio.mp3")

	// 2. Download video
	if err := s.downloadFile(videoURL, videoPath); err != nil {
		return "", fmt.Errorf("failed to download video: %v", err)
	}
	LogJSON(correlationID, "Transcription", "Video downloaded successfully", "INFO")

	// 3. Extract audio using ffmpeg
	// Using exec.Command directly to avoid extra dependencies for simple call if ffmpeg-go is just a wrapper
	cmd := exec.Command("ffmpeg", "-i", videoPath, "-vn", "-acodec", "libmp3lame", "-ac", "1", "-ar", "16000", audioPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ffmpeg error: %v, output: %s", err, string(output))
	}
	LogJSON(correlationID, "Transcription", "Audio extracted successfully", "INFO")

	// 4. Upload to GCS
	objectName := fmt.Sprintf("audio-%d.mp3", time.Now().UnixNano())
	gcsURI := fmt.Sprintf("gs://%s/%s", s.BucketName, objectName)
	
	if err := s.uploadToGCS(ctx, audioPath, objectName); err != nil {
		return "", fmt.Errorf("failed to upload to GCS: %v", err)
	}
	LogJSON(correlationID, "Transcription", fmt.Sprintf("Audio uploaded to GCS: %s", gcsURI), "INFO")
	defer s.deleteFromGCS(ctx, objectName)

	// 5. Asynchronous Transcription
	op, err := s.SpeechClient.LongRunningRecognize(ctx, &speechpb.LongRunningRecognizeRequest{
		Config: &speechpb.RecognitionConfig{
			Encoding:        speechpb.RecognitionConfig_MP3,
			SampleRateHertz: 16000,
			LanguageCode:    "pl-PL", // Default to Polish as per project standard, but maybe detect?
            // Enable automatic punctuation
            EnableAutomaticPunctuation: true,
		},
		Audio: &speechpb.RecognitionAudio{
			AudioSource: &speechpb.RecognitionAudio_Uri{
				Uri: gcsURI,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to start long running recognize: %v", err)
	}

	LogJSON(correlationID, "Transcription", "Waiting for transcription to complete...", "INFO")
	resp, err := op.Wait(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to wait for transcription: %v", err)
	}

	var transcript string
	for _, result := range resp.Results {
		for _, alt := range result.Alternatives {
			transcript += alt.Transcript + " "
		}
	}

	LogJSON(correlationID, "Transcription", "Transcription completed successfully", "INFO")
	return transcript, nil
}

func (s *TranscriptionService) downloadFile(url string, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func (s *TranscriptionService) uploadToGCS(ctx context.Context, localPath string, objectName string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	wc := s.StorageClient.Bucket(s.BucketName).Object(objectName).NewWriter(ctx)
	if _, err = io.Copy(wc, f); err != nil {
		return err
	}
	return wc.Close()
}

func (s *TranscriptionService) deleteFromGCS(ctx context.Context, objectName string) error {
	return s.StorageClient.Bucket(s.BucketName).Object(objectName).Delete(ctx)
}
