package main

import (
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/morikuni/failure"
)

type targetRepository struct {
	RepositoryName string
	RepositoryURL  string
	ruleSetDir     string
}

var targets = []targetRepository{
	{
		"squizlabs/PHP_CodeSniffer",
		"https://github.com/squizlabs/PHP_CodeSniffer",
		"src/Standards/",
	},
}

type CommonNameValue struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type RuleSet struct {
	Name        string             `xml:"name,attr"`
	Description string             `xml:"description"`
	References  []RuleSetReference `xml:"rule"`
	Arg         CommonNameValue    `xml:"arg"`

	Rules            []*Rule
	TargetRepository targetRepository
}

type RuleSetReference struct {
	Name       string            `xml:"ref,attr"`
	Properties RuleSetProperties `xml:"properties"`
	Severity   int               `xml:"severity"`
}

type RuleSetProperties struct {
	Property []CommonNameValue `xml:"property"`
}

type Rule struct {
	Name           string
	Title          string
	Description    string
	CodeComparison []RuleCodeComparison
}

func (r *Rule) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	tmp := struct {
		Title          string               `xml:"title,attr"`
		Description    string               `xml:"standard"`
		CodeComparison []RuleCodeComparison `xml:"code_comparison"`
	}{}
	err := d.DecodeElement(&tmp, &start)
	if err != nil {
		return err
	}

	r.Title = tmp.Title
	r.Description = tmp.Description
	r.CodeComparison = tmp.CodeComparison

	r.Description = strings.TrimSpace(r.Description)

	return nil
}

type RuleCodeComparison struct {
	Code []RuleCode `xml:"code"`
}

type RuleCode struct {
	Title string
	Body  string
}

func (r *RuleCode) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	tmp := struct {
		Title string `xml:"title,attr"`
		Body  string `xml:",cdata"`
	}{}
	err := d.DecodeElement(&tmp, &start)
	if err != nil {
		return err
	}

	r.Title = tmp.Title
	r.Body = tmp.Body

	r.Body = strings.TrimPrefix(r.Body, "\n        \n")
	r.Body = strings.TrimSuffix(r.Body, "\n        ")
	r.Body = strings.TrimSuffix(r.Body, "        ")
	r.Body = strings.ReplaceAll(r.Body, "<em>", "")
	r.Body = strings.ReplaceAll(r.Body, "</em>", "")

	return nil
}

func getTempDir(reponame string) string {
	return fmt.Sprintf("tmp/%s", reponame)
}

func main() {
	ruleSets := make([]*RuleSet, 0)
	for _, target := range targets {
		log.Printf("%s: Cloning", target.RepositoryName)
		tempDir := getTempDir(target.RepositoryName)

		cmd := exec.Command(
			"git",
			"clone",
			"--depth", "1",
			target.RepositoryURL,
			tempDir,
		)
		cmdOut, _ := cmd.StdoutPipe()
		cmdErr, _ := cmd.StderrPipe()
		err := cmd.Start()
		if err != nil {
			log.Fatalf("err: %v", err)
		}
		rr, _ := io.ReadAll(cmdOut)
		log.Printf("%s: out: %s", target.RepositoryName, string(rr))

		rr, _ = io.ReadAll(cmdErr)
		log.Printf("%s: error: %s", target.RepositoryName, string(rr))

		ruleSetDir := filepath.Join(tempDir, target.ruleSetDir)
		files, err := os.ReadDir(ruleSetDir)
		if err != nil {
			log.Fatalf("err: %v", err)
		}

		for _, file := range files {
			ruleSet, err := getRuleSet(file.Name(), filepath.Join(ruleSetDir, file.Name()))
			if err != nil {
				continue
			}
			ruleSet.TargetRepository = target
			ruleSets = append(ruleSets, ruleSet)
			spew.Dump(ruleSet)
		}
	}

	t, err := template.New("index.html").Funcs(template.FuncMap{
		"noescape": func(s string) template.HTML {
			return template.HTML(s)
		},
	}).ParseFiles("template/index.html")
	if err != nil {
		log.Fatalf("err: %v", err)
	}

	writer, err := os.Create("build/index.html")
	if err != nil {
		log.Fatalf("err: %v", err)
	}
	defer writer.Close()

	if err := t.Execute(writer, map[string]interface{}{
		"ruleSets": ruleSets,
	}); err != nil {
		log.Fatalf("err: %v", err)
	}
}

func getRuleSet(ruleSetName string, ruleSetDir string) (*RuleSet, error) {
	reader, err := os.Open(filepath.Join(ruleSetDir, "ruleSet.xml"))
	if err != nil {
		return nil, failure.Wrap(err)
	}
	defer reader.Close()

	result := RuleSet{}
	dec := xml.NewDecoder(reader)
	err = dec.Decode(&result)
	if err != nil {
		return nil, failure.Wrap(err)
	}

	result.Rules, err = getRules(ruleSetName, ruleSetDir)
	if err != nil {
		return nil, failure.Wrap(err)
	}

	return &result, nil
}

func getRules(ruleSetName string, ruleSetDir string) ([]*Rule, error) {
	docsDir := filepath.Join(ruleSetDir, "Docs")
	dirs, err := os.ReadDir(docsDir)
	if err != nil {
		return nil, failure.Wrap(err)
	}

	rules := make([]*Rule, 0)

	for _, dir := range dirs {
		ruleDir := filepath.Join(docsDir, dir.Name())
		files, err := os.ReadDir(ruleDir)
		if err != nil {
			return nil, failure.Wrap(err)
		}

		for _, file := range files {
			reader, err := os.Open(filepath.Join(ruleDir, file.Name()))
			if err != nil {
				return nil, failure.Wrap(err)
			}

			defer reader.Close()
			result := &Rule{}
			dec := xml.NewDecoder(reader)
			err = dec.Decode(&result)
			if err != nil {
				return nil, failure.Wrap(err)
			}

			result.Name = fmt.Sprintf(
				"%s.%s.%s",
				ruleSetName,
				dir.Name(),
				strings.TrimSuffix(file.Name(), "Standard.xml"),
			)

			rules = append(rules, result)
		}
	}

	return rules, nil
}
