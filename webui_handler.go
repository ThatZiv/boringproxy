package main

import (
	"fmt"
	"github.com/GeertJohan/go.rice"
	"html/template"
	"io"
	"log"
	"net/http"
	"strconv"
)

type WebUiHandler struct {
	config *BoringProxyConfig
	db     *Database
	auth   *Auth
	tunMan *TunnelManager

        stylesText string
        indexTemplate *template.Template
        confirmTemplate *template.Template
        loginTemplate *template.Template
}

type IndexData struct {
	Styles  template.CSS
	Tunnels map[string]Tunnel
}

type ConfirmData struct {
	Styles     template.CSS
	Message    string
	ConfirmUrl string
	CancelUrl  string
}

type LoginTemplateData struct {
	Styles     template.CSS
}

func NewWebUiHandler(config *BoringProxyConfig, db *Database, auth *Auth, tunMan *TunnelManager) *WebUiHandler {

        box, err := rice.FindBox("webui")
	if err != nil {
                log.Fatal(err)
	}

	stylesText, err := box.String("styles.css")
	if err != nil {
                log.Fatal(err)
	}

        indexTemplateStr, err := box.String("index.tmpl")
        if err != nil {
                log.Fatal(err)
        }

        indexTemplate, err := template.New("indexhtml").Parse(indexTemplateStr)
        if err != nil {
                log.Fatal(err)
        }

        confirmTemplateStr, err := box.String("confirm.tmpl")
	if err != nil {
                log.Fatal(err)
        }

        confirmTemplate, err := template.New("confirmhtml").Parse(confirmTemplateStr)
        if err != nil {
                log.Fatal(err)
        }

        loginTemplateStr, err := box.String("login.tmpl")
        if err != nil {
                log.Fatal(err)
        }

        loginTemplate, err := template.New("loginhtml").Parse(loginTemplateStr)
        if err != nil {
                log.Fatal(err)
        }

	return &WebUiHandler{
		config,
		db,
		auth,
		tunMan,
                stylesText,
                indexTemplate,
                confirmTemplate,
                loginTemplate,
	}
}

func (h *WebUiHandler) handleWebUiRequest(w http.ResponseWriter, r *http.Request) {

	switch r.URL.Path {
	case "/login":
		h.handleLogin(w, r)
	case "/":

		token, err := extractToken("access_token", r)
		if err != nil {

                        loginTemplateData := LoginTemplateData{
                                Styles:  template.CSS(h.stylesText),
                        }

			w.WriteHeader(401)
                        h.loginTemplate.Execute(w, loginTemplateData)
			return
		}

		if !h.auth.Authorized(token) {
			w.WriteHeader(403)
			w.Write([]byte("Not authorized"))
			return
		}

		indexData := IndexData{
			Styles:  template.CSS(h.stylesText),
			Tunnels: h.db.GetTunnels(),
		}

		h.indexTemplate.Execute(w, indexData)

		//io.WriteString(w, indexTemplate)

	case "/tunnels":

		token, err := extractToken("access_token", r)
		if err != nil {
			w.WriteHeader(401)
			w.Write([]byte("No token provided"))
			return
		}

		if !h.auth.Authorized(token) {
			w.WriteHeader(403)
			w.Write([]byte("Not authorized"))
			return
		}

		h.handleTunnels(w, r)

	case "/confirm-delete-tunnel":

		r.ParseForm()

		if len(r.Form["domain"]) != 1 {
			w.WriteHeader(400)
			w.Write([]byte("Invalid domain parameter"))
			return
		}
		domain := r.Form["domain"][0]

		data := &ConfirmData{
			Styles:     template.CSS(h.stylesText),
			Message:    fmt.Sprintf("Are you sure you want to delete %s?", domain),
			ConfirmUrl: fmt.Sprintf("/delete-tunnel?domain=%s", domain),
			CancelUrl:  "/",
		}

		h.confirmTemplate.Execute(w, data)

	case "/delete-tunnel":
		token, err := extractToken("access_token", r)
		if err != nil {
			w.WriteHeader(401)
			w.Write([]byte("No token provided"))
			return
		}

		if !h.auth.Authorized(token) {
			w.WriteHeader(403)
			w.Write([]byte("Not authorized"))
			return
		}

		r.ParseForm()

		if len(r.Form["domain"]) != 1 {
			w.WriteHeader(400)
			w.Write([]byte("Invalid domain parameter"))
			return
		}
		domain := r.Form["domain"][0]

		h.tunMan.DeleteTunnel(domain)

		http.Redirect(w, r, "/", 307)
	default:
		w.WriteHeader(400)
		w.Write([]byte("Invalid endpoint"))
		return
	}
}

func (h *WebUiHandler) handleTunnels(w http.ResponseWriter, r *http.Request) {

	switch r.Method {
	case "POST":
		h.handleCreateTunnel(w, r)
	default:
		w.WriteHeader(405)
		w.Write([]byte("Invalid method for /tunnels"))
		return
	}
}

func (h *WebUiHandler) handleLogin(w http.ResponseWriter, r *http.Request) {

	switch r.Method {
	case "GET":
		query := r.URL.Query()
		key, exists := query["key"]

		if !exists {
			w.WriteHeader(400)
			fmt.Fprintf(w, "Must provide key for verification")
			return
		}

		token, err := h.auth.Verify(key[0])

		if err != nil {
			w.WriteHeader(400)
			fmt.Fprintf(w, "Invalid key")
			return
		}

		cookie := &http.Cookie{Name: "access_token", Value: token, Secure: true, HttpOnly: true}
		http.SetCookie(w, cookie)

		http.Redirect(w, r, "/", 307)

	case "POST":

		r.ParseForm()

		toEmail, ok := r.Form["email"]

		if !ok {
			w.WriteHeader(400)
			w.Write([]byte("Email required for login"))
			return
		}

		// run in goroutine because it can take some time to send the
		// email
		go h.auth.Login(toEmail[0], h.config)

		io.WriteString(w, "Check your email to finish logging in")
	default:
		w.WriteHeader(405)
		w.Write([]byte("Invalid method for login"))
	}
}

func (h *WebUiHandler) handleCreateTunnel(w http.ResponseWriter, r *http.Request) {

	r.ParseForm()

	if len(r.Form["domain"]) != 1 {
		w.WriteHeader(400)
		w.Write([]byte("Invalid domain parameter"))
		return
	}
	domain := r.Form["domain"][0]

	if len(r.Form["client-name"]) != 1 {
		w.WriteHeader(400)
		w.Write([]byte("Invalid client-name parameter"))
		return
	}
	clientName := r.Form["client-name"][0]

	if len(r.Form["client-port"]) != 1 {
		w.WriteHeader(400)
		w.Write([]byte("Invalid client-port parameter"))
		return
	}

	clientPort, err := strconv.Atoi(r.Form["client-port"][0])
	if err != nil {
		w.WriteHeader(400)
		w.Write([]byte("Invalid client-port parameter"))
		return
	}

	fmt.Println(domain, clientName, clientPort)
	_, err = h.tunMan.CreateTunnelForClient(domain, clientName, clientPort)
	if err != nil {
		w.WriteHeader(400)
		io.WriteString(w, err.Error())
		return
	}

	http.Redirect(w, r, "/", 303)
}
