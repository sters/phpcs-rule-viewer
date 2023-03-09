package main

import (
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/morikuni/failure"
)

type targetRepository struct {
	RepositoryName string
	RepositoryURL  string
	ruleSetsDir    string
}

func (t targetRepository) getTempDirPath() string {
	return filepath.Join("tmp", t.RepositoryName)
}

func (t targetRepository) getRuleSetsDirPath() string {
	return filepath.Join(t.getTempDirPath(), t.ruleSetsDir)
}

func (t targetRepository) getRuleSetDir(ruleSetDir string) string {
	return filepath.Join(t.getRuleSetsDirPath(), ruleSetDir)
}

func (t targetRepository) getRuleSetXmlPath(ruleSetDir string) string {
	return filepath.Join(t.getRuleSetDir(ruleSetDir), "ruleset.xml")
}

func (t targetRepository) getRuleSetDocsDir(ruleSetDir string) string {
	return filepath.Join(t.getRuleSetDir(ruleSetDir), "Docs")
}

func (t targetRepository) getRuleDir(ruleSetDir string, ruleDir string) string {
	return filepath.Join(t.getRuleSetDocsDir(ruleSetDir), ruleDir)
}

func (t targetRepository) getRuleFilePath(ruleSetDir string, ruleDir string, fileName string) string {
	return filepath.Join(t.getRuleDir(ruleSetDir, ruleDir), fileName)
}

func (t targetRepository) gitClone() error {
	cmd := exec.Command(
		"git",
		"clone",
		"--depth", "1",
		t.RepositoryURL,
		t.getTempDirPath(),
	)

	cmdOut, _ := cmd.StdoutPipe()
	cmdErr, _ := cmd.StderrPipe()

	err := cmd.Start()
	if err != nil {
		return failure.Wrap(err)
	}

	rr, _ := io.ReadAll(cmdOut)
	log.Printf("%s: git stdout: %s", t.RepositoryName, string(rr))

	rr, _ = io.ReadAll(cmdErr)
	log.Printf("%s: git stderr: %s", t.RepositoryName, string(rr))

	return nil
}

func (t targetRepository) getRuleSetsDirPaths() ([]fs.DirEntry, error) {
	dirs, err := os.ReadDir(t.getRuleSetsDirPath())
	if err != nil {
		return nil, failure.Wrap(err)
	}

	return dirs, nil
}

func (t targetRepository) getRuleSet(ruleSetName string) (*RuleSet, error) {
	reader, err := os.Open(t.getRuleSetXmlPath(ruleSetName))
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

	result.Rules, err = t.getRules(ruleSetName)
	if err != nil {
		return nil, failure.Wrap(err)
	}

	return &result, nil
}

func (t targetRepository) getRules(ruleSetName string) ([]*Rule, error) {
	dirs, err := os.ReadDir(t.getRuleSetDocsDir(ruleSetName))
	if err != nil {
		return nil, failure.Wrap(err)
	}

	rules := make([]*Rule, 0)

	for _, dir := range dirs {
		files, err := os.ReadDir(t.getRuleDir(ruleSetName, dir.Name()))
		if err != nil {
			return nil, failure.Wrap(err)
		}

		for _, file := range files {
			reader, err := os.Open(t.getRuleFilePath(ruleSetName, dir.Name(), file.Name()))
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

var targets = []targetRepository{
	{
		"squizlabs/PHP_CodeSniffer",
		"https://github.com/squizlabs/PHP_CodeSniffer.git",
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

func main() {
	ruleSets := make([]*RuleSet, 0)
	for _, target := range targets {
		log.Printf("%s: Cloning", target.RepositoryName)
		if err := target.gitClone(); err != nil {
			log.Printf("failed to clone repo: %v", err)
			continue
		}

		dirs, err := target.getRuleSetsDirPaths()
		if err != nil {
			log.Fatalf("err: %v", err)
		}

		for _, dir := range dirs {
			ruleSet, err := target.getRuleSet(dir.Name())
			if err != nil {
				log.Printf("%s: %s: err: %v", target.RepositoryName, dir.Name(), err)
				continue
			}
			ruleSet.TargetRepository = target
			ruleSets = append(ruleSets, ruleSet)
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
