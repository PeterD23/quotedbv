package main

import (
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cj123/formulate"
	"github.com/cj123/formulate/decorators"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"mvdan.cc/xurls"
)

var (
	quotesFolder      string
	password          string
	videosFolder      string
	uploadedVideoName string = "uploadedVideoName"
	websiteDomain     string
	websiteName       string
)

func init() {
	flag.StringVar(&quotesFolder, "f", "quotes", "where to store the quotes")
	flag.StringVar(&password, "p", "password", "password")
	flag.StringVar(&videosFolder, "v", "videos", "where to store the videos")
	flag.StringVar(&websiteDomain, "d", "https://example.com", "website domain starting with https")
	flag.StringVar(&websiteName, "n", "Quotes", "name of the website")
	flag.Parse()
}

func main() {
	if err := os.MkdirAll(quotesFolder, 0755); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(videosFolder, 0755); err != nil {
		panic(err)
	}

	indexTmpl := template.Must(template.New("index").Parse(indexTemplate))
	quoteTmpl := template.Must(template.New("quote").Parse(quoteTemplate))
	uploadTmpl := template.Must(template.New("upload-video").Parse(uploadVideoTemplate))
	incorrectTmpl := template.Must(template.New("incorrect").Parse(incorrectTemplate))

	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		quotes, err := listQuotes()

		if err != nil {
			http.Error(w, "couldn't list quotes", http.StatusInternalServerError)
			return
		}

		_ = indexTmpl.Execute(w, map[string]interface{}{
			"WebsiteName": websiteName,
			"Quotes":      quotes,
		})
	})

	r.HandleFunc("/add-quote", func(w http.ResponseWriter, r *http.Request) {
		var quoteForm AddQuoteForm

		encodedForm, save, err := formulate.Formulate(r, &quoteForm, buildEncoder, buildDecoder)
		sess, currentVideoInSession := GetUploadedVideoFileInSession(w, r)

		if err == nil && save {
			quoteForm.Quote.Time = time.Now()
			quoteForm.Quote.WhatSillyVideoFeaturesTheSillyThing = currentVideoInSession

			if err := quoteForm.Quote.Save(); err != nil {
				http.Error(w, "couldn't save quote", http.StatusInternalServerError)
				return
			}

			sess.SetAttr(uploadedVideoName, "")
			http.Redirect(w, r, "/", http.StatusFound)
		} else if err != nil {
			http.Error(w, "bad form", http.StatusInternalServerError)
			return
		}
		finalTemplate := strings.Replace(addQuoteTemplate, ":video:", currentVideoInSession, -1)
		w.Header().Add("Content-Type", "text/html")
		_, _ = w.Write([]byte(fmt.Sprintf(finalTemplate, encodedForm)))
	})

	r.HandleFunc("/quote", func(w http.ResponseWriter, r *http.Request) {
		quotes, err := listQuotes()
		var quote *Quote
		var keys = slices.Collect(maps.Keys(r.URL.Query()))
		for _, element := range quotes {
			var hash = element.GetHash()
			if keys[0] == hash {
				quote = element
				break
			}
		}

		if quote == nil {
			quote = new(Quote)
			quote.WhatSillyThingDidTheySay = "I'm sorry, this isn't a valid quote"
			quote.Time = time.Now()
			quote.WhoSaidTheSillyThing = "quotedbv"
		}

		var metaTags = quote.GenerateEmbeddableMeta()

		if err != nil {
			http.Error(w, "couldn't list quote", http.StatusInternalServerError)
			return
		}

		_ = quoteTmpl.Execute(w, map[string]interface{}{
			"WebsiteName": websiteName,
			"MetaTags":    metaTags,
			"Quote":       quote,
		})
	})

	r.HandleFunc("/upload-video", func(w http.ResponseWriter, r *http.Request) {
		var errorMessage string
		if r.Method == http.MethodPost {
			err := UploadVideo(w, r)
			if err != nil {
				errorMessage = err.Error()
			}
		}

		_ = uploadTmpl.Execute(w, map[string]interface{}{
			"errorMessage":   errorMessage,
			csrf.TemplateTag: csrf.TemplateField(r),
		})
	})

	r.Get("/incorrect", func(w http.ResponseWriter, r *http.Request) {
		_ = incorrectTmpl.Execute(w, map[string]interface{}{
			csrf.TemplateTag: csrf.TemplateField(r),
		})
	})

	r.Get("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/plain")
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /"))
	})

	FileServer(r, "/embed", http.Dir(videosFolder))

	b := make([]byte, 32)

	_, err := rand.Read(b)

	if err != nil {
		panic(err)
	}

	log.Fatal(http.ListenAndServeTLS(":10443", "certificate.crt", "private.key", csrf.Protect(b)(r)))
}

func buildEncoder(r *http.Request, w io.Writer) *formulate.HTMLEncoder {
	enc := formulate.NewEncoder(w, r, decorators.BootstrapDecorator{})
	enc.SetCSRFProtection(true)
	enc.SetFormat(false)

	return enc
}

func buildDecoder(r *http.Request, form url.Values) *formulate.HTTPDecoder {
	dec := formulate.NewDecoder(form)
	dec.SetValueOnValidationError(false)
	dec.AddValidators(passwordValidator{})

	return dec
}

func listQuotes() ([]*Quote, error) {
	files, err := os.ReadDir(quotesFolder)

	if err != nil {
		return nil, err
	}

	var quotes []*Quote

	for _, file := range files {
		q, err := readQuote(file.Name())

		if err != nil {
			return nil, err
		}

		quotes = append(quotes, q)
	}

	sort.Slice(quotes, func(i, j int) bool {
		return quotes[i].Time.After(quotes[j].Time)
	})

	return quotes, nil
}

func readQuote(file string) (*Quote, error) {
	f, err := os.Open(filepath.Join(quotesFolder, file))

	if err != nil {
		return nil, err
	}

	defer f.Close()

	var quote Quote

	if err := json.NewDecoder(f).Decode(&quote); err != nil {
		return nil, err
	}

	return &quote, nil
}

type Quote struct {
	Time                                time.Time `show:"-"`
	WhoSaidTheSillyThing                string    `name:"Who said the silly thing?"`
	WhatSillyThingDidTheySay            string    `name:"What silly thing did they say?" elem:"textarea"`
	WhatSillyVideoFeaturesTheSillyThing string    `show:"-"`
}

func (q *Quote) GenerateEmbeddableMeta() template.HTML {
	var hash = q.GetHash()

	var siteName = fmt.Sprintf("<meta property='og:site_name' content='%s'>", html.EscapeString(websiteName))
	var websiteUrl = fmt.Sprintf("<meta property='og:url' content='%s/quote?%s'>", websiteDomain, hash)
	var pageTitle = fmt.Sprintf("<meta property='og:title' content='%s'>", html.EscapeString(websiteName))
	var pageDescription = fmt.Sprintf("<meta property='og:description' content='\"%s\" ~%s'>", q.WhatSillyThingDidTheySay, q.WhoSaidTheSillyThing)
	var embedType = "<meta property='og:type' content='website'>"

	var basicMeta = fmt.Sprintf("%s\n%s\n%s\n%s", siteName, websiteUrl, pageTitle, pageDescription)
	var videoName = q.WhatSillyVideoFeaturesTheSillyThing
	if videoName != "" {
		if _, err := os.Stat(filepath.Join(videosFolder, videoName)); errors.Is(err, os.ErrNotExist) {
			return template.HTML(fmt.Sprintf("%s\n%s", basicMeta, embedType))
		}
		embedType = "<meta property='og:type' content='video.other'>"
		var videoString = fmt.Sprintf("<meta property='og:video' content='%s/embed/%s'>", websiteDomain, videoName)
		var contentType = fmt.Sprintf("<meta property='og:video:type' content='video/%s'>", LastElement(videoName, "."))
		var videoDimensions = "<meta property='og:video:width' content='1280'>\n<meta property='og:video:height' content='720'>"

		return template.HTML(fmt.Sprintf("%s\n%s\n%s\n%s\n%s", basicMeta, embedType, videoString, contentType, videoDimensions))
	}
	return template.HTML(fmt.Sprintf("%s\n%s", basicMeta, embedType))
}

func (q *Quote) EmbedVideo() template.HTML {
	var videoName = q.WhatSillyVideoFeaturesTheSillyThing
	if videoName != "" {
		if _, err := os.Stat(filepath.Join(videosFolder, videoName)); errors.Is(err, os.ErrNotExist) {
			return template.HTML("<br><p>There was a video here, but its gone :(</p>")
		}
		var contentType = LastElement(videoName, ".")
		return template.HTML(fmt.Sprintf("<br><video width='80%%' height='80%%' style='border: 1px solid #fff; margin-top: 20px; margin-bottom: 20px' controls preload='metadata'><source src='../embed/%s' type='video/%s'>", videoName, contentType))
	}
	return template.HTML("<br>")
}

func (q *Quote) IsImageURL() bool {
	_, err := url.Parse(q.WhatSillyThingDidTheySay)

	return err == nil && strings.Contains(q.WhatSillyThingDidTheySay, "http") &&
		(strings.HasSuffix(q.WhatSillyThingDidTheySay, ".png") || strings.HasSuffix(q.WhatSillyThingDidTheySay, ".jpg") || strings.HasSuffix(q.WhatSillyThingDidTheySay, ".jpeg") || strings.HasSuffix(q.WhatSillyThingDidTheySay, ".gif"))
}

func (q *Quote) HTML(list bool) template.HTML {
	if q.IsImageURL() {
		return template.HTML(fmt.Sprintf(`<img src="%s" class="img img-fluid" style="max-height: 400px;">`, q.WhatSillyThingDidTheySay))
	}
	q.WhatSillyThingDidTheySay = xurls.Relaxed.ReplaceAllString(q.WhatSillyThingDidTheySay, `<a href="$1">$1</a>`)

	var link = q.GetHash()
	var multiline = strings.Replace(q.WhatSillyThingDidTheySay, "\r\n", "<br>", -1)

	if list {
		return template.HTML(fmt.Sprintf("<a href='/quote?%s'>%s</a>", link, multiline))
	} else {
		return template.HTML(multiline)
	}
}

func (q *Quote) Save() error {
	f, err := os.Create(filepath.Join(quotesFolder, fmt.Sprintf("%s.json", q.Time.Format("2006-01-02_15-04-05"))))

	if err != nil {
		return err
	}

	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "    ")
	enc.SetEscapeHTML(false)
	return enc.Encode(q)
}

func (q *Quote) GetHash() string {
	var hash = sha256.Sum256([]byte(fmt.Sprintf("%v", q)))
	return string(hex.EncodeToString(hash[:])[:16])
}

type AddQuoteForm struct {
	Quote
	WhatIsThePassword formulate.Password `name:"What is the password?" help:"If you don't know this, then you don't belong here." validators:"password"`
}

var (
	//go:embed templates/index.html
	indexTemplate string

	//go:embed templates/quote.html
	quoteTemplate string

	//go:embed templates/add-quote.html
	addQuoteTemplate string

	//go:embed templates/upload-video.html
	uploadVideoTemplate string

	//go:embed templates/incorrect.html
	incorrectTemplate string
)

type passwordValidator struct{}

func (p passwordValidator) Validate(val interface{}) (ok bool, message string) {
	switch a := val.(type) {
	case string:
		if a == password {
			return true, ""
		}

		return false, "The password is incorrect."
	default:
		return false, ""
	}
}

func (p passwordValidator) TagName() string {
	return "password"
}
