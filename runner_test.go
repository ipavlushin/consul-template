package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	dep "github.com/hashicorp/consul-template/dependency"
	"github.com/hashicorp/consul-template/test"
	"github.com/hashicorp/consul-template/watch"
)

func TestNewRunner_initialize(t *testing.T) {
	in1 := test.CreateTempfile(nil, t)
	defer test.DeleteTempfile(in1, t)

	in2 := test.CreateTempfile(nil, t)
	defer test.DeleteTempfile(in2, t)

	in3 := test.CreateTempfile(nil, t)
	defer test.DeleteTempfile(in3, t)

	dry, once := true, true
	config := &Config{
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{Source: in1.Name(), Command: "1"},
			&ConfigTemplate{Source: in1.Name(), Command: "1.1"},
			&ConfigTemplate{Source: in2.Name(), Command: "2"},
			&ConfigTemplate{Source: in3.Name(), Command: "3"},
		},
	}

	runner, err := NewRunner(config, dry, once)
	if err != nil {
		t.Fatal(err)
	}

	if runner.config != config {
		t.Errorf("expected %#v to be %#v", runner.config, config)
	}

	if runner.dry != dry {
		t.Errorf("expected %#v to be %#v", runner.dry, dry)
	}

	if runner.once != once {
		t.Errorf("expected %#v to be %#v", runner.once, once)
	}

	if runner.client == nil {
		t.Errorf("expected %#v to be %#v", runner.client, nil)
	}

	if runner.watcher == nil {
		t.Errorf("expected %#v to be %#v", runner.watcher, nil)
	}

	if num := len(runner.templates); num != 3 {
		t.Errorf("expected %d to be %d", len(runner.templates), 3)
	}

	if runner.renderedTemplates == nil {
		t.Errorf("expected %#v to be %#v", runner.renderedTemplates, nil)
	}

	if num := len(runner.ctemplatesMap); num != 3 {
		t.Errorf("expected %d to be %d", len(runner.ctemplatesMap), 3)
	}

	ctemplates := runner.ctemplatesMap[in1.Name()]
	if num := len(ctemplates); num != 2 {
		t.Errorf("expected %d to be %d", len(ctemplates), 2)
	}

	if runner.outStream != os.Stdout {
		t.Errorf("expected %#v to be %#v", runner.outStream, os.Stdout)
	}

	brain := NewBrain()
	if !reflect.DeepEqual(runner.brain, brain) {
		t.Errorf("expected %#v to be %#v", runner.brain, brain)
	}

	if runner.ErrCh == nil {
		t.Errorf("expected %#v to be %#v", runner.ErrCh, nil)
	}

	if runner.DoneCh == nil {
		t.Errorf("expected %#v to be %#v", runner.DoneCh, nil)
	}
}

func TestNewRunner_badTemplate(t *testing.T) {
	config := &Config{
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{Source: "/not/a/real/path"},
		},
	}

	if _, err := NewRunner(config, false, false); err == nil {
		t.Fatal("expected error, but nothing was returned")
	}
}

func TestNewRunner_setsOutStream(t *testing.T) {
	runner, err := NewRunner(new(Config), false, false)
	if err != nil {
		t.Fatal(err)
	}

	buff := new(bytes.Buffer)
	runner.SetOutStream(buff)

	if runner.outStream != buff {
		t.Errorf("expected %q to equal %q", runner.outStream, buff)
	}
}

func TestNewRunner_setsErrStream(t *testing.T) {
	runner, err := NewRunner(new(Config), false, false)
	if err != nil {
		t.Fatal(err)
	}

	buff := new(bytes.Buffer)
	runner.SetErrStream(buff)

	if runner.errStream != buff {
		t.Errorf("expected %q to equal %q", runner.errStream, buff)
	}
}

func TestReceive_addsToBrain(t *testing.T) {
	runner, err := NewRunner(new(Config), false, false)
	if err != nil {
		t.Fatal(err)
	}

	d, err := dep.ParseStoreKey("foo")
	if err != nil {
		t.Fatal(err)
	}

	data := "some value"
	runner.Receive(d, data)

	value := runner.brain.storeKeys[d.HashCode()]
	if data != value {
		t.Errorf("expected %q to be %q", data, value)
	}
}

func TestReceive_storesBrain(t *testing.T) {
	runner, err := NewRunner(new(Config), false, false)
	if err != nil {
		t.Fatal(err)
	}

	d, data := &dep.File{}, "this is some data"
	runner.Receive(d, data)

	if !runner.brain.Remembered(d) {
		t.Errorf("expected brain to have data")
	}
}

func TestRun_noopIfMissingData(t *testing.T) {
	in := test.CreateTempfile([]byte(`
    {{ range service "consul@nyc1" }}{{ end }}
  `), t)
	defer test.DeleteTempfile(in, t)

	config := &Config{
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{Source: in.Name()},
		},
	}

	runner, err := NewRunner(config, false, false)
	if err != nil {
		t.Fatal(err)
	}

	buff := new(bytes.Buffer)
	runner.SetOutStream(buff)

	if err := runner.Run(); err != nil {
		t.Fatal(err)
	}

	if num := len(buff.Bytes()); num != 0 {
		t.Errorf("expected %d to be %d", num, 0)
	}
}

func TestRun_dry(t *testing.T) {
	in := test.CreateTempfile([]byte(`
    {{ range service "consul@nyc1" }}{{.Node}}{{ end }}
  `), t)
	defer test.DeleteTempfile(in, t)

	config := &Config{
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{
				Source:      in.Name(),
				Destination: "/out/file.txt",
			},
		},
	}

	runner, err := NewRunner(config, true, false)
	if err != nil {
		t.Fatal(err)
	}

	d, err := dep.ParseHealthServices("consul@nyc1")
	if err != nil {
		t.Fatal(err)
	}
	data := []*dep.HealthService{
		&dep.HealthService{Node: "consul1"},
		&dep.HealthService{Node: "consul2"},
	}
	runner.Receive(d, data)

	buff := new(bytes.Buffer)
	runner.SetOutStream(buff)

	if err := runner.Run(); err != nil {
		t.Fatal(err)
	}

	actual := bytes.TrimSpace(buff.Bytes())
	expected := bytes.TrimSpace([]byte(`
    > /out/file.txt

    consul1consul2
  `))
	if !bytes.Equal(actual, expected) {
		t.Errorf("expected \n%q\n to equal \n%q\n", actual, expected)
	}
}

func TestRun_singlePass(t *testing.T) {
	in := test.CreateTempfile([]byte(`
    {{ range service "consul@nyc1"}}{{ end }}
    {{ range service "consul@nyc2"}}{{ end }}
    {{ range service "consul@nyc3"}}{{ end }}
  `), t)
	defer test.DeleteTempfile(in, t)

	config := &Config{
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{Source: in.Name()},
		},
	}

	runner, err := NewRunner(config, true, false)
	if err != nil {
		t.Fatal(err)
	}

	if len(runner.dependencies) != 0 {
		t.Errorf("expected %d to be %d", len(runner.dependencies), 0)
	}

	if err := runner.Run(); err != nil {
		t.Fatal(err)
	}

	if len(runner.dependencies) != 3 {
		t.Errorf("expected %d to be %d", len(runner.dependencies), 3)
	}
}

func TestRun_singlePassDuplicates(t *testing.T) {
	in := test.CreateTempfile([]byte(`
    {{ range service "consul@nyc1"}}{{ end }}
    {{ range service "consul@nyc1"}}{{ end }}
    {{ range service "consul@nyc1"}}{{ end }}
    {{ range service "consul@nyc2"}}{{ end }}
    {{ range service "consul@nyc2"}}{{ end }}
    {{ range service "consul@nyc3"}}{{ end }}
    {{ range service "consul@nyc3"}}{{ end }}
  `), t)
	defer test.DeleteTempfile(in, t)

	config := &Config{
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{Source: in.Name()},
		},
	}

	runner, err := NewRunner(config, true, false)
	if err != nil {
		t.Fatal(err)
	}

	if len(runner.dependencies) != 0 {
		t.Errorf("expected %d to be %d", len(runner.dependencies), 0)
	}

	if err := runner.Run(); err != nil {
		t.Fatal(err)
	}

	if len(runner.dependencies) != 3 {
		t.Errorf("expected %d to be %d", len(runner.dependencies), 3)
	}
}

func TestRun_doublePass(t *testing.T) {
	in := test.CreateTempfile([]byte(`
		{{ range ls "services" }}
			{{ range service .Key }}
				{{.Node}} {{.Address}}:{{.Port}}
			{{ end }}
		{{ end }}
  `), t)
	defer test.DeleteTempfile(in, t)

	config := &Config{
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{Source: in.Name()},
		},
	}

	runner, err := NewRunner(config, true, false)
	if err != nil {
		t.Fatal(err)
	}

	if len(runner.dependencies) != 0 {
		t.Errorf("expected %d to be %d", len(runner.dependencies), 0)
	}

	if err := runner.Run(); err != nil {
		t.Fatal(err)
	}

	if len(runner.dependencies) != 1 {
		t.Errorf("expected %d to be %d", len(runner.dependencies), 1)
	}

	d, err := dep.ParseStoreKeyPrefix("services")
	if err != nil {
		t.Fatal(err)
	}
	data := []*dep.KeyPair{
		&dep.KeyPair{Key: "service1"},
		&dep.KeyPair{Key: "service2"},
		&dep.KeyPair{Key: "service3"},
	}
	runner.Receive(d, data)

	if err := runner.Run(); err != nil {
		t.Fatal(err)
	}

	if len(runner.dependencies) != 4 {
		t.Errorf("expected %d to be %d", len(runner.dependencies), 4)
	}
}

func TestRun_removesUnusedDependencies(t *testing.T) {
	in := test.CreateTempfile([]byte(nil), t)
	defer test.DeleteTempfile(in, t)

	config := &Config{
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{Source: in.Name()},
		},
	}

	runner, err := NewRunner(config, true, false)
	if err != nil {
		t.Fatal(err)
	}

	d, err := dep.ParseHealthServices("consul@nyc2")
	if err != nil {
		t.Fatal(err)
	}

	runner.dependencies = []dep.Dependency{d, d, d}

	if err := runner.Run(); err != nil {
		t.Fatal(err)
	}

	if len(runner.dependencies) != 0 {
		t.Errorf("expected %d to be %d", len(runner.dependencies), 0)
	}

	if runner.watcher.Watching(d) {
		t.Errorf("expected watcher to stop watching dependency")
	}

	if runner.brain.Remembered(d) {
		t.Errorf("expected brain to forget dependency")
	}
}

func TestRun_multipleTemplatesRunsCommands(t *testing.T) {
	in1 := test.CreateTempfile([]byte(`
    {{ range service "consul@nyc1" }}{{.Node}}{{ end }}
  `), t)
	defer test.DeleteTempfile(in1, t)

	in2 := test.CreateTempfile([]byte(`
    {{range service "consul@nyc2"}}{{.Node}}{{ end }}
  `), t)
	defer test.DeleteTempfile(in2, t)

	out1 := test.CreateTempfile(nil, t)
	test.DeleteTempfile(out1, t)

	out2 := test.CreateTempfile(nil, t)
	test.DeleteTempfile(out2, t)

	touch1, err := ioutil.TempFile(os.TempDir(), "touch1-")
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(touch1.Name())
	defer os.Remove(touch1.Name())

	touch2, err := ioutil.TempFile(os.TempDir(), "touch2-")
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(touch2.Name())
	defer os.Remove(touch2.Name())

	config := &Config{
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{
				Source:      in1.Name(),
				Destination: out1.Name(),
				Command:     fmt.Sprintf("touch %s", touch1.Name()),
			},
			&ConfigTemplate{
				Source:      in2.Name(),
				Destination: out2.Name(),
				Command:     fmt.Sprintf("touch %s", touch2.Name()),
			},
		},
	}

	runner, err := NewRunner(config, false, false)
	if err != nil {
		t.Fatal(err)
	}

	d, err := dep.ParseHealthServices("consul@nyc1")
	if err != nil {
		t.Fatal(err)
	}
	data := []*dep.HealthService{
		&dep.HealthService{Node: "consul1"},
		&dep.HealthService{Node: "consul2"},
	}
	runner.Receive(d, data)

	if err := runner.Run(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(touch1.Name()); err != nil {
		t.Errorf("expected first command to run, but did not: %s", err)
	}

	if _, err := os.Stat(touch2.Name()); err == nil {
		t.Errorf("expected second command to not run, but touch exists")
	}
}

// Warning: this is a super fragile and time-dependent test. If it's failing,
// check the demo Consul cluster and your own sanity before you assume your
// code broke something...
func TestRunner_quiescence(t *testing.T) {
	in := test.CreateTempfile([]byte(`
    {{ range service "consul" "any" }}{{.Node}}{{ end }}
  `), t)
	defer test.DeleteTempfile(in, t)

	out := test.CreateTempfile(nil, t)
	test.DeleteTempfile(out, t)

	config := &Config{
		Consul: "demo.consul.io",
		Wait: &watch.Wait{
			Min: 500 * time.Millisecond,
			Max: 1 * time.Second,
		},
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{
				Source:      in.Name(),
				Destination: out.Name(),
			},
		},
	}

	runner, err := NewRunner(config, false, false)
	if err != nil {
		t.Fatal(err)
	}

	go runner.Start()
	defer runner.Stop()

	min := time.After(400 * time.Millisecond)
	max := time.After(1 * time.Second)
	for {
		select {
		case <-min:
			if _, err = os.Stat(out.Name()); !os.IsNotExist(err) {
				t.Errorf("expected quiescence timer to not fire for yet")
			}
			continue
		case <-max:
			if _, err = os.Stat(out.Name()); os.IsNotExist(err) {
				t.Errorf("expected template to be rendered by now")
			}
			return
		}
	}
}

func TestRender_sameContentsDoesNotExecuteCommand(t *testing.T) {
	outFile := test.CreateTempfile(nil, t)
	os.Remove(outFile.Name())
	defer os.Remove(outFile.Name())

	inTemplate := test.CreateTempfile([]byte(`
    {{ range service "consul@nyc1" }}{{.Node}}{{ end }}
  `), t)
	defer test.DeleteTempfile(inTemplate, t)

	outTemplate := test.CreateTempfile([]byte(`
    consul1consul2
  `), t)
	defer test.DeleteTempfile(outTemplate, t)

	config := &Config{
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{
				Source:      inTemplate.Name(),
				Destination: outTemplate.Name(),
				Command:     fmt.Sprintf("echo 'foo' > %s", outFile.Name()),
			},
		},
	}

	runner, err := NewRunner(config, false, false)
	if err != nil {
		t.Fatal(err)
	}

	d, err := dep.ParseHealthServices("consul@nyc1")
	if err != nil {
		t.Fatal(err)
	}
	data := []*dep.HealthService{
		&dep.HealthService{Node: "consul1"},
		&dep.HealthService{Node: "consul2"},
	}
	runner.Receive(d, data)

	if err := runner.Run(); err != nil {
		t.Fatal(err)
	}

	_, err = os.Stat(outFile.Name())
	if !os.IsNotExist(err) {
		t.Fatalf("expected command to not be run")
	}
}

func TestAtomicWrite_parentFolderMissing(t *testing.T) {
	// Create a TempDir and a TempFile in that TempDir, then remove them to
	// "simulate" a non-existent folder
	outDir, err := ioutil.TempDir(os.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outDir)
	outFile, err := ioutil.TempFile(outDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(outDir); err != nil {
		t.Fatal(err)
	}

	if err := atomicWrite(outFile.Name(), nil); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(outFile.Name()); err != nil {
		t.Fatal(err)
	}
}

func TestAtomicWrite_retainsPermissions(t *testing.T) {
	outDir, err := ioutil.TempDir(os.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outDir)
	outFile, err := ioutil.TempFile(outDir, "")
	if err != nil {
		t.Fatal(err)
	}
	os.Chmod(outFile.Name(), 0644)

	if err := atomicWrite(outFile.Name(), nil); err != nil {
		t.Fatal(err)
	}

	stat, err := os.Stat(outFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	expected := os.FileMode(0644)
	if stat.Mode() != expected {
		t.Errorf("expected %q to be %q", stat.Mode(), expected)
	}
}

func TestRun_doesNotExecuteCommandMissingDependencies(t *testing.T) {
	outFile := test.CreateTempfile(nil, t)
	os.Remove(outFile.Name())
	defer os.Remove(outFile.Name())

	inTemplate := test.CreateTempfile([]byte(`
    {{ range service "consul@nyc1"}}{{ end }}
  `), t)
	defer test.DeleteTempfile(inTemplate, t)

	outTemplate := test.CreateTempfile(nil, t)
	defer test.DeleteTempfile(outTemplate, t)

	config := &Config{
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{
				Source:      inTemplate.Name(),
				Destination: outTemplate.Name(),
				Command:     fmt.Sprintf("echo 'foo' > %s", outFile.Name()),
			},
		},
	}

	runner, err := NewRunner(config, false, false)
	if err != nil {
		t.Fatal(err)
	}

	if err := runner.Run(); err != nil {
		t.Fatal(err)
	}

	_, err = os.Stat(outFile.Name())
	if !os.IsNotExist(err) {
		t.Fatalf("expected command to not be run")
	}
}

func TestRun_executesCommand(t *testing.T) {
	outFile := test.CreateTempfile(nil, t)
	os.Remove(outFile.Name())
	defer os.Remove(outFile.Name())

	inTemplate := test.CreateTempfile([]byte(`
    {{ range service "consul@nyc1"}}{{ end }}
  `), t)
	defer test.DeleteTempfile(inTemplate, t)

	outTemplate := test.CreateTempfile(nil, t)
	defer test.DeleteTempfile(outTemplate, t)

	config := &Config{
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{
				Source:      inTemplate.Name(),
				Destination: outTemplate.Name(),
				Command:     fmt.Sprintf("echo 'foo' > %s", outFile.Name()),
			},
		},
	}

	runner, err := NewRunner(config, false, false)
	if err != nil {
		t.Fatal(err)
	}

	d, err := dep.ParseHealthServices("consul@nyc1")
	if err != nil {
		t.Fatal(err)
	}
	data := []*dep.HealthService{
		&dep.HealthService{
			Node:    "consul",
			Address: "1.2.3.4",
			ID:      "consul@nyc1",
			Name:    "consul",
		},
	}
	runner.Receive(d, data)

	if err := runner.Run(); err != nil {
		t.Fatal(err)
	}

	_, err = os.Stat(outFile.Name())
	if err != nil {
		t.Fatal(err)
	}
}

func TestRun_doesNotExecuteCommandMoreThanOnce(t *testing.T) {
	outFile := test.CreateTempfile(nil, t)
	os.Remove(outFile.Name())
	defer os.Remove(outFile.Name())

	inTemplate := test.CreateTempfile([]byte(`
    {{ range service "consul@nyc1"}}{{ end }}
  `), t)
	defer test.DeleteTempfile(inTemplate, t)

	outTemplateA := test.CreateTempfile(nil, t)
	defer test.DeleteTempfile(outTemplateA, t)

	outTemplateB := test.CreateTempfile(nil, t)
	defer test.DeleteTempfile(outTemplateB, t)

	config := &Config{
		ConfigTemplates: []*ConfigTemplate{
			&ConfigTemplate{
				Source:      inTemplate.Name(),
				Destination: outTemplateA.Name(),
				Command:     fmt.Sprintf("echo 'foo' >> %s", outFile.Name()),
			},
			&ConfigTemplate{
				Source:      inTemplate.Name(),
				Destination: outTemplateB.Name(),
				Command:     fmt.Sprintf("echo 'foo' >> %s", outFile.Name()),
			},
		},
	}

	runner, err := NewRunner(config, false, false)
	if err != nil {
		t.Fatal(err)
	}

	d, err := dep.ParseHealthServices("consul@nyc1")
	if err != nil {
		t.Fatal(err)
	}
	data := []*dep.HealthService{
		&dep.HealthService{
			Node:    "consul",
			Address: "1.2.3.4",
			ID:      "consul@nyc1",
			Name:    "consul",
		},
	}
	runner.Receive(d, data)

	if err := runner.Run(); err != nil {
		t.Fatal(err)
	}

	_, err = os.Stat(outFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	output, err := ioutil.ReadFile(outFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(string(output), "foo") > 1 {
		t.Fatalf("expected command to be run once.")
	}
}

func TestBuildConfig_singleFile(t *testing.T) {
	configFile := test.CreateTempfile([]byte(`
		consul = "127.0.0.1"
	`), t)
	defer test.DeleteTempfile(configFile, t)

	config := new(Config)
	if err := buildConfig(config, configFile.Name()); err != nil {
		t.Fatal(err)
	}

	expected := "127.0.0.1"
	if config.Consul != expected {
		t.Errorf("expected %q to be %q", config.Consul, expected)
	}
}

func TestBuildConfig_NonExistentDirectory(t *testing.T) {
	// Create a directory and then delete it
	configDir, err := ioutil.TempDir(os.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(configDir); err != nil {
		t.Fatal(err)
	}

	config := new(Config)
	err = buildConfig(config, configDir)
	if err == nil {
		t.Fatalf("expected error, but nothing was returned")
	}

	expected := "missing file/folder"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected %q to contain %q", err.Error(), expected)
	}
}

func TestBuildConfig_EmptyDirectory(t *testing.T) {
	// Create a directory with no files
	configDir, err := ioutil.TempDir(os.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(configDir)

	config := new(Config)
	err = buildConfig(config, configDir)
	if err == nil {
		t.Fatalf("expected error, but nothing was returned")
	}

	expected := "must contain at least one configuration file"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected %q to contain %q", err.Error(), expected)
	}
}

func TestBuildConfig_BadConfigs(t *testing.T) {
	configFile := test.CreateTempfile([]byte(`
		totally not a vaild config
	`), t)
	defer test.DeleteTempfile(configFile, t)

	configDir := filepath.Dir(configFile.Name())

	config := new(Config)
	err := buildConfig(config, configDir)
	if err == nil {
		t.Fatalf("expected error, but nothing was returned")
	}

	expected := "1 error(s) occurred"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected %q to contain %q", err.Error(), expected)
	}
}

func TestBuildConfig_configDir(t *testing.T) {
	configDir, err := ioutil.TempDir(os.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	configFile1, err := ioutil.TempFile(configDir, "")
	if err != nil {
		t.Fatal(err)
	}
	config1 := []byte(`
		consul = "127.0.0.1:8500"
	`)
	_, err = configFile1.Write(config1)
	if err != nil {
		t.Fatal(err)
	}
	configFile2, err := ioutil.TempFile(configDir, "")
	if err != nil {
		t.Fatal(err)
	}
	config2 := []byte(`
		template {
		  source = "/path/on/disk/to/template"
		  destination = "/path/on/disk/where/template/will/render"
		  command = "optional command to run when the template is updated"
		}
	`)
	_, err = configFile2.Write(config2)
	if err != nil {
		t.Fatal(err)
	}

	config := new(Config)
	if err := buildConfig(config, configDir); err != nil {
		t.Fatal(err)
	}

	expectedConfig := Config{
		Consul: "127.0.0.1:8500",
		ConfigTemplates: []*ConfigTemplate{{
			Source:      "/path/on/disk/to/template",
			Destination: "/path/on/disk/where/template/will/render",
			Command:     "optional command to run when the template is updated",
		}},
	}
	if expectedConfig.Consul != config.Consul {
		t.Fatalf("Config files failed to combine. Expected Consul to be %s but got %s", expectedConfig.Consul, config.Consul)
	}
	if len(config.ConfigTemplates) != len(expectedConfig.ConfigTemplates) {
		t.Fatalf("Expected %d ConfigTemplate but got %d", len(expectedConfig.ConfigTemplates), len(config.ConfigTemplates))
	}
	for i, expectTemplate := range expectedConfig.ConfigTemplates {
		actualTemplate := config.ConfigTemplates[i]
		if actualTemplate.Source != expectTemplate.Source {
			t.Fatalf("Expected template Source to be %s but got %s", expectTemplate.Source, actualTemplate.Source)
		}
		if actualTemplate.Destination != expectTemplate.Destination {
			t.Fatalf("Expected template Destination to be %s but got %s", expectTemplate.Destination, actualTemplate.Destination)
		}
		if actualTemplate.Command != expectTemplate.Command {
			t.Fatalf("Expected template Command to be %s but got %s", expectTemplate.Command, actualTemplate.Command)
		}
	}
}
