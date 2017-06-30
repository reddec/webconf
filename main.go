package main

import (
	"github.com/go-ini/ini"
	"net/http"
	"html/template"
	"bytes"
	"flag"
	"github.com/julienschmidt/httprouter"
	"github.com/satori/go.uuid"
	"encoding/json"
	"io/ioutil"
	"strings"
	"os"
	"path"
)

type Item struct {
	Key   string
	Value string
}

type Group struct {
	Name   string
	Values []Item
}

type Params struct {
	Default  []Item
	Sections []Group
	Filename string
}

func getIniFile(filename string) (Params, error) {
	var res Params
	f := ini.Empty()
	err := f.Append(filename)
	if err != nil {
		return res, nil
	}

	for _, sec := range f.Sections() {
		var group Group
		group.Name = sec.Name()

		for _, k := range sec.Keys() {
			group.Values = append(group.Values, Item{k.Name(), k.Value()})
		}
		if sec.Name() != "DEFAULT" {
			res.Sections = append(res.Sections, group)
		} else {
			res.Default = group.Values
		}
	}
	res.Filename = filename
	return res, nil
}

func saveIniFile(filename string, res Params) error {
	f := ini.Empty()
	s, err := f.NewSection("DEFAULT")
	if err != nil {
		return err
	}
	for _, kv := range res.Default {
		_, err = s.NewKey(kv.Key, kv.Value)
		if err != nil {
			return err
		}
	}
	for _, section := range res.Sections {
		s, err := f.NewSection(section.Name)
		if err != nil {
			return err
		}
		for _, kv := range section.Values {
			_, err = s.NewKey(kv.Key, kv.Value)
			if err != nil {
				return err
			}
		}
	}
	return f.SaveTo(filename)
}

type View struct {
	Params            *Params
	UUID              string
	Links             []string
	AllowCreate       bool
	AllowDelete       bool
	AllowSaveTemplate bool
}

func (v *View) NextUUID() string {
	v.UUID = uuid.NewV4().String()
	return v.UUID
}

func scanFiles() []string {
	stats, err := ioutil.ReadDir(".")
	if err != nil {
		return nil
	}
	var res []string
	for _, f := range stats {
		if strings.HasSuffix(f.Name(), ".ini") {
			res = append(res, f.Name())
		}
	}
	return res
}

func renderPage(params *Params) (string, error) {
	data, _ := staticTemplateHtmlBytes()
	t, err := template.New("").Delims("{%", "%}").Funcs(template.FuncMap{"json": func(v interface{}) template.JS {
		a, _ := json.Marshal(v)
		return template.JS(a)
	},
	}).Parse(string(data))
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	err = t.Execute(buf, &View{Params: params,
		UUID:                          uuid.NewV4().String(),
		Links:                         scanFiles(),
		AllowCreate:                   !*disableCreate,
		AllowDelete:                   !*disableDelete,
		AllowSaveTemplate:             !*disableSaveTemplate,
	})

	return buf.String(), err
}

var (
	disableCreate       = flag.Bool("disable-create", false, "Disable create new configuration file")
	disableDelete       = flag.Bool("disable-delete", false, "Disable remove configuration file")
	disableSaveTemplate = flag.Bool("disable-save-template", false, "Disable save as template")
	templatesDir        = flag.String("template", "templates", "Templates directory")
	bind                = flag.String("bind", ":9000", "HTTP Binding")
	workdir             = flag.String("w", ".", "Working directory")
)

//go:generate go-bindata-assetfs static/...
func main() {
	flag.Parse()
	err := os.Chdir(*workdir)
	if err != nil {
		panic(err)
	}
	router := httprouter.New()
	router.ServeFiles("/static/*filepath", assetFS())
	router.GET("/data/:filename", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		file := params.ByName("filename")
		prm, err := getIniFile(file)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		ec := json.NewEncoder(writer)
		ec.SetIndent("", "    ")
		ec.Encode(prm)
	})
	router.POST("/data/:filename", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		file := params.ByName("filename")
		if _, err := os.Stat(file); os.IsNotExist(err) && *disableCreate {
			http.Error(writer, err.Error(), http.StatusForbidden)
			return
		}
		if request.Header.Get("Content-Type") != "application/json" {
			http.Error(writer, "application/json required", http.StatusBadRequest)
			return
		}
		var cfg Params
		err := json.NewDecoder(request.Body).Decode(&cfg)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		err = saveIniFile(file, cfg)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})

	router.GET("/templates", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		stats, _ := ioutil.ReadDir(*templatesDir)
		var names []string
		for _, s := range stats {
			names = append(names, s.Name())
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		ec := json.NewEncoder(writer)
		ec.SetIndent("", "    ")
		ec.Encode(names)
	})

	router.POST("/template/:filename", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		file := params.ByName("filename")
		if _, err := os.Stat(file); os.IsNotExist(err) && *disableSaveTemplate {
			http.Error(writer, err.Error(), http.StatusForbidden)
			return
		}
		if request.Header.Get("Content-Type") != "application/json" {
			http.Error(writer, "application/json required", http.StatusBadRequest)
			return
		}
		var cfg Params
		err := json.NewDecoder(request.Body).Decode(&cfg)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		err = os.MkdirAll(*templatesDir, 0755)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		err = saveIniFile(path.Join(*templatesDir, file), cfg)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})

	router.DELETE("/template/:filename", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		file := params.ByName("filename")
		if *disableDelete {
			http.Error(writer, "Access forbidden", http.StatusForbidden)
			return
		}
		err := os.Remove(path.Join(*templatesDir, file))
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusNoContent)

	})

	router.POST("/by-template/:filename", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		file := params.ByName("filename")
		dest := request.URL.Query().Get("dest")
		if _, err := os.Stat(file); os.IsNotExist(err) && *disableCreate {
			http.Error(writer, err.Error(), http.StatusForbidden)
			return
		}

		prm, err := getIniFile(path.Join(*templatesDir, file))
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}

		err = saveIniFile(dest, prm)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})

	router.DELETE("/data/:filename", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		file := params.ByName("filename")
		if *disableDelete {
			http.Error(writer, "Access forbidden", http.StatusForbidden)
			return
		}
		err := os.Remove(file)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusNoContent)

	})

	router.GET("/editor/:filename", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		file := params.ByName("filename")

		prm, err := getIniFile(file)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		text, err := renderPage(&prm)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "text/html")
		writer.WriteHeader(http.StatusOK)
		writer.Write([]byte(text))
	})
	http.Handle("/", router)
	panic(http.ListenAndServe(*bind, nil))
}
