package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	jsonerror "github.com/ddymko/go-jsonerror"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// isSupportedAudioFormat checks if the audio format (format) is a supported
// audio format, returning true of so, or false otherwise
func isSupportedAudioFormat(format string) bool {
	supportedAudioFormats := []string{"aac", "flac", "mp3"}
	return slices.Contains(supportedAudioFormats, strings.ToLower(format))
}

func appError(w http.ResponseWriter, err error) {
	log.Println(err)
	var error jsonerror.ErrorJSON
	error.AddError(jsonerror.ErrorComp{
		Detail: err.Error(),
		Code:   strconv.Itoa(http.StatusBadRequest),
		Title:  "Something went wrong converting the audio file",
		Status: http.StatusBadRequest,
	})
	http.Error(w, error.Error(), http.StatusBadRequest)
}

// convertAudio converts an audio file from one format to one of AAC, FLAC, or
// MP3, and returns the converted audio file data in a *bytes.Buffer
func convertAudio(file *os.File, format string, path string) ([]byte, error) {
	var args ffmpeg.KwArgs
	switch strings.ToLower(format) {
	case "aac":
		args = ffmpeg.KwArgs{"c:a": "libfdk_aa", "f": "aac"}
	case "flac":
		args = ffmpeg.KwArgs{"c:a": "flac", "f": "flac"}
	default:
		args = ffmpeg.KwArgs{"c:a": "libmp3lame", "f": "mp3"}
	}

	// Create a temporary file to store the converted audio file
	audioFile, err := os.CreateTemp(filepath.Join(path, "data/convert-tmp"), "audio-file-*."+format)
	if err != nil {
		return nil, err
	}
	defer audioFile.Close()

	// Convert the file to a different audio format
	err = ffmpeg.
		Input(file.Name()).
		Output(audioFile.Name(), args).
		OverWriteOutput().
		ErrorToStdOut().
		Run()

	if err != nil {
		return nil, err
	}

	fileinfo, err := audioFile.Stat()
	if err != nil {
		return nil, err
	}

	// Write the contents of the converted file to a byte array and return it
	buffer := make([]byte, fileinfo.Size())
	_, err = audioFile.Read(buffer)
	if err != nil {
		return nil, err
	}

	return buffer, nil
}

// convertAudioFile converts audio files from one format to another.
// Specifically, it accepts POST requests containing an audio file (audio_file)
// in the POST data, and the format to convert it to in a route attribute
// (to_format). If the received file is an audio file, it then uses FFMpeg to
// convert the file to the specified format, saving the file with an
// auto-generated, temporary filename. After the audio file is converted, it is
// sent back to the client, and the original audio file is deleted.
func convertAudioFile(w http.ResponseWriter, r *http.Request) {
	path, err := os.Getwd()
	if err != nil {
		appError(w, err)
		return
	}

	format := r.PathValue("to_format")
	if !isSupportedAudioFormat(format) {
		appError(w, fmt.Errorf("%s is not a supported audio format", format))
		return
	}
	log.Printf("desired audio format is %s", format)

	// Parse the form setting a file size limit of 10MB
	r.ParseMultipartForm(10 << 20)

	// Retrieve the audio file from the POST data
	file, handler, err := r.FormFile("audio_file")
	if err != nil {
		appError(w, fmt.Errorf("error retrieving the audio file from the request. reason: %s", err))
		return
	}
	defer file.Close()
	log.Printf("retrieved audio file from the request; name: %s", handler.Filename)

	// Create a temporary file from the uploaded audio file
	tempFile, err := os.CreateTemp(
		filepath.Join(path, "data/upload-tmp"), "audio-file-*"+filepath.Ext(handler.Filename),
	)
	if err != nil {
		appError(w, fmt.Errorf("could not buffer the uploaded audio file. reason: %s", err))
		return
	}
	defer tempFile.Close()
	defer os.Remove(tempFile.Name())

	// Write the uploaded file contents to the temporary file
	nb_bytes, err := io.Copy(tempFile, file)
	if err != nil {
		appError(w, fmt.Errorf("error copying the uploaded audio file from the request. reason: %s", err))
		return
	}
	log.Printf("copied the uploaded audio file to %s (total size: %d bytes)", tempFile.Name(), nb_bytes)

	// Convert the file to a different audio format
	buffer, err := convertAudio(tempFile, format, path)
	if err != nil {
		appError(w, fmt.Errorf("could not convert the uploaded audio file. reason: %s", err))
		return
	}
	log.Printf("Received audio file buffer with %d bytes", len(buffer))

	downloadFilename := strings.TrimSuffix(handler.Filename, filepath.Ext(handler.Filename)) + "." + format
	w.Header().Add("Content-Type", "audio/"+format)
	w.Header().Set("Content-Disposition", "attachment; filename="+downloadFilename)
	w.Write(buffer)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /convert/{to_format}", convertAudioFile)

	log.Print("Starting server on :8080")
	err := http.ListenAndServe(":8080", mux)
	log.Fatal(err)
}
