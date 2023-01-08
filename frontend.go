package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	errors "git.sequentialread.com/forest/pkg-errors"

	"github.com/shengdoushi/base58"
)

type Session struct {
	SessionId        string
	UserID           string
	ExpiresUnixMilli int64
	Flash            *map[string]string
}

type FrontendApp struct {
	Port              int
	Domain            string
	Router            *http.ServeMux
	HTMLTemplates     map[string]*template.Template
	cssHash           string
	basicURLPathRegex *regexp.Regexp
	base58Regex       *regexp.Regexp
	roomNameCache     map[string]string
}

type MatrixRoom struct {
	Id         string
	Name       string
	IdWithName string
	Rows       int
}

func initFrontend(config *Config) FrontendApp {

	cssBytes, err := os.ReadFile(filepath.Join(".", "frontend", "static", "app.css"))
	if err != nil {
		panic(errors.Wrap(err, "can't initFrontend because can't read cssBytes:"))
	}
	hashArray := sha256.Sum256(cssBytes)
	cssHash := base58.Encode(hashArray[:6], base58.BitcoinAlphabet)

	app := FrontendApp{
		Port:              config.FrontendPort,
		Domain:            config.FrontendDomain,
		Router:            http.NewServeMux(),
		HTMLTemplates:     map[string]*template.Template{},
		basicURLPathRegex: regexp.MustCompile("(?i)[a-z0-9/?&_+-]+"),
		base58Regex:       regexp.MustCompile("(?i)[a-z0-9_-]+"),
		cssHash:           cssHash,
		roomNameCache:     map[string]string{},
	}

	// serve the homepage
	app.handleWithSession("/", func(responseWriter http.ResponseWriter, request *http.Request, session Session) {

		userIsLoggedIn := session.UserID != ""
		if userIsLoggedIn {
			if request.Method == "POST" {
				for i := 0; i < 20; i++ {
					roomId := request.PostFormValue(fmt.Sprintf("id_%d", i))
					delete := request.PostFormValue(fmt.Sprintf("delete_%d", i))
					ban := request.PostFormValue(fmt.Sprintf("ban_%d", i))

					log.Printf("%s %s %s", roomId, delete, ban)
				}
			}

			diskUsage, err := os.ReadFile("data/diskUsage.json")
			if err != nil {
				(*session.Flash)["error"] = "an error occurred reading diskUsage json"
			}
			dbTableSizes, err := os.ReadFile("data/dbTableSizes.json")
			if err != nil {
				(*session.Flash)["error"] = "an error occurred reading dbTableSizes json"
			}

			rowCountByRoomObject, err := ReadJsonFile[map[string]int]("data/stateGroupsStateRowCountByRoom.json")
			if err != nil {
				(*session.Flash)["error"] = "an error occurred reading rowCountByRoom json object"
			}

			roomsSlice := []MatrixRoom{}
			totalRowCount := 0
			for roomId, rows := range rowCountByRoomObject {
				totalRowCount += rows
				if rows > 10000 {
					roomsSlice = append(roomsSlice, MatrixRoom{
						Id:   roomId,
						Rows: rows,
					})
				}
			}
			sort.Slice(roomsSlice, func(i, j int) bool {
				return roomsSlice[i].Rows > roomsSlice[j].Rows
			})

			biggestRooms := roomsSlice[0:10]
			bigRoomsRowCount := 0
			for i, room := range biggestRooms {
				// TODO cache this ??
				name, err := matrixAdmin.GetRoomName(room.Id)
				if err != nil {
					log.Printf("error getting name for '%s':  %s\n", room.Id, err)
				}
				biggestRooms[i] = MatrixRoom{
					Id:         room.Id,
					Name:       name,
					IdWithName: fmt.Sprintf("%s: %s", room.Id, name),
					Rows:       room.Rows,
				}
				bigRoomsRowCount += room.Rows
			}

			biggestRooms = append(biggestRooms, MatrixRoom{
				Name: "Others",
				Rows: totalRowCount - bigRoomsRowCount,
			})

			bigRoomsBytes, _ := json.Marshal(biggestRooms)
			//log.Println(string(bigRoomsBytes))

			panelTemplateData := struct {
				DiskUsage     template.JS
				DBTableSizes  template.JS
				BigRooms      template.JS
				BigRoomsSlice []MatrixRoom
			}{template.JS(diskUsage), template.JS(dbTableSizes), template.JS(bigRoomsBytes), biggestRooms}

			app.buildPageFromTemplate(responseWriter, request, session, "panel.html", panelTemplateData)
		} else {
			if request.Method == "POST" {
				username := request.PostFormValue("username")
				password := request.PostFormValue("password")

				success, err := matrixAdmin.Login(username, password)
				if err != nil {
					(*session.Flash)["error"] += "an error was thrown by the login process 😧"
					log.Println(errors.Wrap(err, "an error was thrown by the login process"))
				} else {
					if success {
						session.UserID = username
						session.ExpiresUnixMilli = time.Now().Add(time.Hour * 24).UnixMilli()
						err = app.setSession(responseWriter, &session)
						if err != nil {
							log.Println(errors.Wrap(err, "setSession failed"))
						}
						http.Redirect(responseWriter, request, "/", http.StatusFound)
						return

					} else {
						(*session.Flash)["error"] += "username or password was incorrect"
					}
				}
			}

			loginPageTemplateData := struct {
				MatrixServerPublicDomain string
			}{config.MatrixServerPublicDomain}

			app.buildPageFromTemplate(responseWriter, request, session, "login.html", loginPageTemplateData)
		}
	})

	app.handleWithSession("/logout", func(responseWriter http.ResponseWriter, request *http.Request, session Session) {
		if session.UserID != "" && session.SessionId != "" {
			os.Remove(fmt.Sprintf("data/sessions/%s.json", session.SessionId))
		}
		app.deleteCookie(responseWriter, "sessionId")
		http.Redirect(responseWriter, request, "/", http.StatusFound)
	})

	// registerHowtoRoutes(&app)

	// registerLoginRoutes(&app, emailService)

	// registerProfileRoutes(&app)

	// registerAdminPanelRoutes(&app)

	app.reloadTemplates()

	staticFilesDir := "./frontend/static"
	log.Printf("serving static files from %s", staticFilesDir)
	app.Router.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticFilesDir))))

	return app
}

func (app *FrontendApp) ListenAndServe() error {
	return http.ListenAndServe(fmt.Sprintf(":%d", app.Port), app.Router)
}

func (app *FrontendApp) setCookie(responseWriter http.ResponseWriter, name, value string, lifetimeSeconds int, sameSite http.SameSite) {
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Cookies#define_where_cookies_are_sent
	// The Domain attribute specifies which hosts are allowed to receive the cookie.
	// If unspecified, it defaults to the same host that set the cookie, excluding subdomains.
	// If Domain is specified, then subdomains are always included.
	// Therefore, specifying Domain is less restrictive than omitting it.
	// However, it can be helpful when subdomains need to share information about a user.

	toSet := &http.Cookie{
		Name:     name,
		HttpOnly: true,
		Secure:   true,
		SameSite: sameSite,
		Path:     "/",
		Value:    value,
		MaxAge:   lifetimeSeconds,
	}

	http.SetCookie(responseWriter, toSet)
}

func (app *FrontendApp) deleteCookie(responseWriter http.ResponseWriter, name string) {
	http.SetCookie(responseWriter, &http.Cookie{
		Name:     name,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		Value:    "",
		MaxAge:   -1,
	})
}

func (app *FrontendApp) getSession(request *http.Request, domain string) (Session, error) {
	toReturn := Session{
		Flash: &(map[string]string{}),
	}
	for _, cookie := range request.Cookies() {
		if cookie.Name == "sessionId" && app.base58Regex.MatchString(cookie.Value) {
			session, err := ReadJsonFile[Session](fmt.Sprintf("data/sessions/%s.json", cookie.Value))

			if err == nil {
				if session.ExpiresUnixMilli > time.Now().UnixMilli() {
					toReturn.SessionId = cookie.Value
					toReturn.UserID = session.UserID
				}
			}
			//log.Printf("toReturn.SessionId %s\n", toReturn.SessionId)
		} else if cookie.Name == "flash" && cookie.Value != "" {
			bytes, err := base64.RawURLEncoding.DecodeString(cookie.Value)
			if err != nil {
				log.Printf("can't getSession because can't base64 decode flash cookie: %+v", err)
				return toReturn, err
			}
			flash := map[string]string{}
			err = json.Unmarshal(bytes, &flash)
			if err != nil {
				log.Printf("can't getSession because can't json parse the decoded flash cookie: %+v", err)
				return toReturn, err
			}
			toReturn.Flash = &flash
		}
	}
	return toReturn, nil
}

func (app *FrontendApp) setSession(responseWriter http.ResponseWriter, session *Session) error {
	sessionIdBuffer := make([]byte, 32)
	rand.Read(sessionIdBuffer)
	sessionId := base58.Encode(sessionIdBuffer, base58.BitcoinAlphabet)
	session.SessionId = sessionId

	err := WriteJsonFile(fmt.Sprintf("data/sessions/%s.json", sessionId), *session)
	if err != nil {
		return err
	}

	// bytes, _ := json.MarshalIndent(session, "", "  ")
	// log.Printf("setSession(): %s  %s\n", sessionId, string(bytes))

	exipreInSeconds := int(time.Until(time.UnixMilli(session.ExpiresUnixMilli)).Seconds())
	app.setCookie(responseWriter, "sessionId", sessionId, exipreInSeconds, http.SameSiteStrictMode)

	return nil
}

func (app *FrontendApp) unhandledError(responseWriter http.ResponseWriter, request *http.Request, err error) {
	log.Printf("500 internal server error: %+v\n", err)

	responseWriter.Header().Add("Content-Type", "text/plain")
	responseWriter.WriteHeader(http.StatusInternalServerError)
	responseWriter.Write([]byte("500 internal server error"))
}

func (app *FrontendApp) handleWithSession(path string, handler func(http.ResponseWriter, *http.Request, Session)) {
	app.Router.HandleFunc(path, func(responseWriter http.ResponseWriter, request *http.Request) {
		session, err := app.getSession(request, app.Domain)

		//bytes, _ := json.MarshalIndent(session, "", "  ")
		//log.Printf("handleWithSession(): %s\n", string(bytes))

		if err != nil {
			app.unhandledError(responseWriter, request, err)
		} else {
			handler(responseWriter, request, session)
		}
	})
}

func (app *FrontendApp) buildPage(responseWriter http.ResponseWriter, request *http.Request, session Session, highlight, page template.HTML) {
	var buffer bytes.Buffer
	templateName := "page.html"
	pageTemplate, hasPageTemplate := app.HTMLTemplates[templateName]
	if !hasPageTemplate {
		panic(fmt.Errorf("template '%s' not found!", templateName))
	}
	err := pageTemplate.Execute(
		&buffer,
		struct {
			Session   Session
			Highlight template.HTML
			Page      template.HTML
			CSSHash   string
		}{session, highlight, page, app.cssHash},
	)
	app.deleteCookie(responseWriter, "flash")

	if err != nil {
		app.unhandledError(responseWriter, request, err)
	} else {
		io.Copy(responseWriter, &buffer)
	}
}

func (app *FrontendApp) renderTemplateToHTML(templateName string, data interface{}) (template.HTML, error) {
	var buffer bytes.Buffer
	desiredTemplate, hasTemplate := app.HTMLTemplates[templateName]
	if !hasTemplate {
		return "", fmt.Errorf("template '%s' not found!", templateName)
	}
	err := desiredTemplate.Execute(&buffer, data)
	if err != nil {
		return "", err
	}
	return template.HTML(buffer.String()), nil
}

func (app *FrontendApp) buildPageFromTemplate(responseWriter http.ResponseWriter, request *http.Request, session Session, templateName string, data interface{}) {
	content, err := app.renderTemplateToHTML(templateName, data)
	if err != nil {
		app.unhandledError(responseWriter, request, err)
	} else {
		app.buildPage(responseWriter, request, session, template.HTML(""), content)
	}
}

func (app *FrontendApp) setFlash(responseWriter http.ResponseWriter, session Session, key, value string) {
	(*session.Flash)[key] += value
	bytes, err := json.Marshal((*session.Flash))
	if err != nil {
		log.Printf("can't setFlash because can't json marshal the flash map: %+v", err)
		return
	}

	app.setCookie(responseWriter, "flash", base64.RawURLEncoding.EncodeToString(bytes), 60, http.SameSiteStrictMode)
}

func (app *FrontendApp) reloadTemplates() {

	loadTemplate := func(filename string) *template.Template {
		newTemplateString, err := os.ReadFile(filename)
		if err != nil {
			panic(err)
		}
		newTemplate, err := template.New(filename).Parse(string(newTemplateString))
		if err != nil {
			panic(err)
		}
		return newTemplate
	}

	frontendDirectory := "./frontend"
	//frontendVersion = hashTemplateAndStaticFiles(frontendDirectory)[:6]

	fileInfos, err := os.ReadDir(frontendDirectory)
	if err != nil {
		panic(err)
	}
	for _, fileInfo := range fileInfos {
		if !fileInfo.IsDir() && strings.Contains(fileInfo.Name(), ".gotemplate") {
			app.HTMLTemplates[strings.Replace(fileInfo.Name(), ".gotemplate", "", 1)] = loadTemplate(filepath.Join(frontendDirectory, fileInfo.Name()))
		}
	}

}
