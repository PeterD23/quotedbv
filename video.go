package main

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/icza/session"
	"gopkg.in/vansante/go-ffprobe.v2"
)

func UploadVideo(w http.ResponseWriter, r *http.Request) error {
	r.ParseMultipartForm(10 << 20)

	if r.PostFormValue("password") != password {
		http.Redirect(w, r, "/incorrect", http.StatusFound)
		return fmt.Errorf("incorrect password")
	}

	file, handler, err := r.FormFile("myFile")
	if err != nil {
		return fmt.Errorf("couldn't upload video")
	}
	defer file.Close()

	// Check that the file is a valid video type
	var contentType = handler.Header.Get("Content-Type")
	if !slices.Contains([]string{"video/webm", "video/mp4"}, contentType) {
		return fmt.Errorf("unsupported filetype '%s', please choose .mp4 or .webm", contentType)
	}

	// Check that the file uses a valid embeddable codec
	valid, codec := ValidCodec(file)
	if !valid {
		return fmt.Errorf("unsupported mp4 codec '%s', please choose h264, vp8, vp9 or av1", codec)
	}

	tempFile, err := os.CreateTemp(videosFolder, fmt.Sprintf("*_%s", handler.Filename))
	if err != nil {
		fmt.Println(err)
	}
	defer tempFile.Close()

	uploadedFileName := LastElement(tempFile.Name(), string(os.PathSeparator))
	_, _ = file.Seek(0, 0) // since ffprobe reads the file object, it alters the seek pointer
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		fmt.Println(err)
	}

	session, currentVideoInSession := GetUploadedVideoFileInSession(w, r)
	// Deletes the previous file in the session cache if a quote wasn't added
	if currentVideoInSession != "" {
		os.Remove(filepath.Join(videosFolder, currentVideoInSession))
	}

	// Write the new video file to disk, set the session video file
	tempFile.Write(fileBytes)
	session.SetAttr(uploadedVideoName, uploadedFileName)
	http.Redirect(w, r, "/add-quote", http.StatusFound)
	return nil
}

func FileServer(r chi.Router, path string, root http.FileSystem) {
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit any URL parameters.")
	}

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", http.StatusMovedPermanently).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.Get(path, func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.RouteContext(r.Context())
		pathPrefix := strings.TrimSuffix(rctx.RoutePattern(), "/*")
		fs := http.StripPrefix(pathPrefix, http.FileServer(root))
		fs.ServeHTTP(w, r)
	})
}

func LastElement(str string, sep string) string {
	split := strings.Split(str, sep)
	return split[len(split)-1]
}

func ValidCodec(file multipart.File) (valid bool, codec string) {
	ctx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()

	data, err := ffprobe.ProbeReader(ctx, file)
	if err != nil {
		return false, ""
	}

	var codecName = data.FirstVideoStream().CodecName
	return slices.Contains([]string{"h264", "vp8", "vp9", "av1"}, codecName), codecName
}

func GetUploadedVideoFileInSession(w http.ResponseWriter, r *http.Request) (s session.Session, v string) {
	sess := session.Get(r)
	if sess == nil {
		sess = session.NewSessionOptions(&session.SessOptions{
			Attrs: map[string]interface{}{uploadedVideoName: ""},
		})
		session.Add(sess, w)
	}
	var currentVideoInSession = sess.Attr(uploadedVideoName).(string)
	return sess, currentVideoInSession
}
