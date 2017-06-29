package main

import (
	"github.com/go-ini/ini"
	"net/http"
	"html/template"
	"bytes"
	"flag"
	"github.com/julienschmidt/httprouter"
	"strings"
)

type data map[string]string

type Group struct {
	Name   string
	Values data
}

type Params struct {
	Default  data
	Sections []Group
}

func getIniFile(filename string) (Params, error) {
	var res Params
	f := ini.Empty()
	err := f.Append(filename)
	if err != nil {
		return res, nil
	}
	for _, sec := range f.Sections() {
		var vals data = make(data)
		for _, k := range sec.Keys() {
			vals[k.Name()] = k.Value()
		}
		if sec.Name() != "DEFAULT" {
			res.Sections = append(res.Sections, Group{Name: sec.Name(), Values: vals})
		} else {
			res.Default = vals
		}
	}
	return res, nil
}

func saveIniFile(filename string, res Params) error {
	f := ini.Empty()
	s, err := f.NewSection("DEFAULT")
	if err != nil {
		return err
	}
	for k, v := range res.Default {
		_, err = s.NewKey(k, v)
		if err != nil {
			return err
		}
	}
	for _, section := range res.Sections {
		s, err := f.NewSection(section.Name)
		if err != nil {
			return err
		}
		for k, v := range section.Values {
			_, err = s.NewKey(k, v)
			if err != nil {
				return err
			}
		}
	}
	return f.SaveTo(filename)
}

func renderPage(params *Params) (string, error) {
	data, _ := staticTemplateHtmlBytes()
	t, err := template.New("").Parse(string(data))
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	err = t.Execute(buf, params)
	return buf.String(), err
}

//go:generate go-bindata-assetfs static/
func main() {
	bind := flag.String("bind", ":9000", "HTTP Binding")
	flag.Parse()

	router := httprouter.New()
	router.Handler("GET", "/static", http.FileServer(assetFS()))
	router.GET("/edit/:filename", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
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
	router.POST("/edit/:filename", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		file := params.ByName("filename")
		err := request.ParseForm()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		var cfg Params
		for k, v := range request.Form {
			sv := strings.SplitN(k, "/", 2)
			sectionName, type_ := sv[0], sv[1]
			if type_ == "name" {
				var section Group
				section.Name = sectionName
				section.Values = make(data)
				values := request.Form[sectionName+"/value"]
				for i, key := range v {
					if i < len(values) {
						value := values[i]
						section.Values[key] = value
					}
				}
				if sectionName == "" {
					cfg.Default = section.Values
				} else {
					cfg.Sections = append(cfg.Sections, section)
				}
			}
		}
		err = saveIniFile(file, cfg)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusOK)
		writer.Write([]byte("<html><body><a href='" + file + "'> Save. Return to edit</a></body></html>" ))

	})
	http.Handle("/", router)
	panic(http.ListenAndServe(*bind, nil))
}
