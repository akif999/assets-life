// Copyright (C) 2019 Ichinose Shogo All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in https://github.com/shogo82148/assets-life/blob/master/LICENSE

// assets-life is a very simple embedding asset generator.
// It generates an embed small in-memory file system that is served from an http.FileSystem.
// Install the command line tool first.
//
//     go get github.com/shogo82148/assets-life
//
// The assets-life command generates a package that have embed small in-memory file system.
//
//     assets-life /path/to/your/project/public public
//
// You can access the file system by accessing a public variable Root of the generated package.
//
//     import (
//         "net/http"
//         "./public" // TODO: Replace with the absolute import path
//     )
//
//     func main() {
//         http.Handle("/", http.FileServer(public.Root))
//         http.ListenAndServe(":8080", nil)
//     }
//
// Visit http://localhost:8080/path/to/file to see your file.
//
// The assets-life command also embed go:generate directive into generated code, and assets-life itself.
// It allows you to re-generate the package using go generate.
//
//     go generate ./public
//
// The assets-life command is no longer needed because it is embedded into the generated package.
package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) <= 2 {
		log.Println("Usage:")
		log.Println(os.Args[0] + " INPUT_DIR OUTPUT_DIR [PACKAGE_NAME]")
		os.Exit(2)
	}
	in, err := filepath.Abs(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	out, err := filepath.Abs(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
	var name string
	if len(os.Args) > 3 {
		name = os.Args[3]
	}
	if name == "" {
		name = filepath.Base(out)
	}
	if err := build(in, out, name); err != nil {
		log.Fatal(err)
	}
}

func build(in, out, name string) error {
	filename := "assets-life.go"
	rel, err := filepath.Rel(out, in)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(out, "filesystem.go"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	header := `// Code generated by go run %s. DO NOT EDIT.

//%s

package %s

import (
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// Root is the root of the file system.
var Root http.FileSystem = fileSystem{
`
	rel = filepath.ToSlash(rel)
	fmt.Fprintf(f, header, filename, "go:generate go run "+filename+" \""+rel+"\" . "+name, name)

	type file struct {
		path     string
		mode     os.FileMode
		children []int
		next     int
	}
	index := map[string]int{}
	files := []file{}

	var i int
	err = filepath.Walk(in, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// ignore hidden files
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		if (info.Mode()&os.ModeType)|os.ModeDir != os.ModeDir {
			return fmt.Errorf("unsupported file type: %s, mode %s", path, info.Mode())
		}

		index[path] = i
		files = append(files, file{
			path: path,
			mode: info.Mode(),
		})
		parent := filepath.Dir(path)
		if idx, ok := index[parent]; ok {
			files[idx].children = append(files[idx].children, i)
		}
		i++
		return nil
	})
	if err != nil {
		return err
	}

	for _, ff := range files {
		// search neighborhood
		for i := range ff.children {
			next := -1
			if i+1 < len(ff.children) {
				next = ff.children[i+1]
			}
			files[ff.children[i]].next = next
		}

		fmt.Fprintf(f, "\tfile{\n")
		rel, err := filepath.Rel(in, ff.path)
		if err != nil {
			return err
		}
		fmt.Fprintf(f, "\t\tname:    %q,\n", path.Clean("/"+filepath.ToSlash(rel)))
		if ff.mode.IsDir() {
			fmt.Fprintln(f, "\t\tcontent: \"\",")
		} else {
			b, err := ioutil.ReadFile(ff.path)
			if err != nil {
				return err
			}
			fmt.Fprintf(f, "\t\tcontent: %q,\n", string(b))
		}
		switch {
		case ff.mode.IsDir(): // directory
			fmt.Fprintln(f, "\t\tmode:    0755 | os.ModeDir,")
		case ff.mode&0100 != 0: // executable file
			fmt.Fprintln(f, "\t\tmode:    0755,")
		default:
			fmt.Fprintln(f, "\t\tmode:    0644,")
		}
		fmt.Fprintf(f, "\t\tnext:    %d,\n", ff.next)
		if len(ff.children) > 0 {
			fmt.Fprintf(f, "\t\tchild:   %d,\n", ff.children[0])
		} else {
			fmt.Fprint(f, "\t\tchild:   -1,\n")
		}
		fmt.Fprint(f, "\t},\n")
	}
	footer := `}

type fileSystem []file

func (fs fileSystem) Open(name string) (http.File, error) {
	i := sort.Search(len(fs), func(i int) bool { return fs[i].name >= name })
	if i >= len(fs) || fs[i].name != name {
		return nil, &os.PathError{
			Op:   "open",
			Path: name,
			Err:  os.ErrNotExist,
		}
	}
	f := &fs[i]
	return &httpFile{
		Reader: strings.NewReader(f.content),
		file:   f,
		fs:     fs,
		idx:    i,
		dirIdx: f.child,
	}, nil
}

type file struct {
	name    string
	content string
	mode    os.FileMode
	child   int
	next    int
}

var _ os.FileInfo = (*file)(nil)

func (f *file) Name() string {
	return path.Base(f.name)
}

func (f *file) Size() int64 {
	return int64(len(f.content))
}

func (f *file) Mode() os.FileMode {
	return f.mode
}

var zeroTime time.Time

func (f *file) ModTime() time.Time {
	return zeroTime
}

func (f *file) IsDir() bool {
	return f.Mode().IsDir()
}

func (f *file) Sys() interface{} {
	return nil
}

type httpFile struct {
	*strings.Reader
	file   *file
	fs     fileSystem
	idx    int
	dirIdx int
}

var _ http.File = (*httpFile)(nil)

func (f *httpFile) Stat() (os.FileInfo, error) {
	return f.file, nil
}

func (f *httpFile) Readdir(count int) ([]os.FileInfo, error) {
	ret := []os.FileInfo{}
	if !f.file.IsDir() {
		return ret, nil
	}

	if count <= 0 {
		for f.dirIdx >= 0 {
			entry := &f.fs[f.dirIdx]
			ret = append(ret, entry)
			f.dirIdx = entry.next
		}
		return ret, nil
	}

	ret = make([]os.FileInfo, 0, count)
	for f.dirIdx >= 0 {
		entry := &f.fs[f.dirIdx]
		ret = append(ret, entry)
		f.dirIdx = entry.next
		if len(ret) == count {
			return ret, nil
		}
	}
	return ret, io.EOF
}

func (f *httpFile) Close() error {
	return nil
}`
	fmt.Fprintln(f, footer)
	if err := f.Close(); err != nil {
		return err
	}

	f, err = os.OpenFile(filepath.Join(out, filename), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	format := `// Copyright (C) 2019 Ichinose Shogo All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in https://github.com/shogo82148/assets-life/blob/master/LICENSE

// +build ignore

// assets-life is a very simple embedding asset generator.
// It generates an embed small in-memory file system that is served from an http.FileSystem.
// Install the command line tool first.
//
//     go get github.com/shogo82148/assets-life
//
// The assets-life command generates a package that have embed small in-memory file system.
//
//     assets-life /path/to/your/project/public public
//
// You can access the file system by accessing a public variable Root of the generated package.
//
//     import (
//         "net/http"
//         "./public" // TODO: Replace with the absolute import path
//     )
//
//     func main() {
//         http.Handle("/", http.FileServer(public.Root))
//         http.ListenAndServe(":8080", nil)
//     }
//
// Visit http://localhost:8080/path/to/file to see your file.
//
// The assets-life command also embed go:generate directive into generated code, and assets-life itself.
// It allows you to re-generate the package using go generate.
//
//     go generate ./public
//
// The assets-life command is no longer needed because it is embedded into the generated package.
package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) <= 2 {
		log.Println("Usage:")
		log.Println(os.Args[0] + " INPUT_DIR OUTPUT_DIR [PACKAGE_NAME]")
		os.Exit(2)
	}
	in, err := filepath.Abs(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	out, err := filepath.Abs(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
	var name string
	if len(os.Args) > 3 {
		name = os.Args[3]
	}
	if name == "" {
		name = filepath.Base(out)
	}
	if err := build(in, out, name); err != nil {
		log.Fatal(err)
	}
}

func build(in, out, name string) error {
	filename := "assets-life.go"
	rel, err := filepath.Rel(out, in)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(out, "filesystem.go"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	header := %c%s%c
	rel = filepath.ToSlash(rel)
	fmt.Fprintf(f, header, filename, "go:generate go run "+filename+" \""+rel+"\" . "+name, name)

	type file struct {
		path     string
		mode     os.FileMode
		children []int
		next     int
	}
	index := map[string]int{}
	files := []file{}

	var i int
	err = filepath.Walk(in, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// ignore hidden files
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		if (info.Mode()&os.ModeType)|os.ModeDir != os.ModeDir {
			return fmt.Errorf("unsupported file type: %%s, mode %%s", path, info.Mode())
		}

		index[path] = i
		files = append(files, file{
			path: path,
			mode: info.Mode(),
		})
		parent := filepath.Dir(path)
		if idx, ok := index[parent]; ok {
			files[idx].children = append(files[idx].children, i)
		}
		i++
		return nil
	})
	if err != nil {
		return err
	}

	for _, ff := range files {
		// search neighborhood
		for i := range ff.children {
			next := -1
			if i+1 < len(ff.children) {
				next = ff.children[i+1]
			}
			files[ff.children[i]].next = next
		}

		fmt.Fprintf(f, "\tfile{\n")
		rel, err := filepath.Rel(in, ff.path)
		if err != nil {
			return err
		}
		fmt.Fprintf(f, "\t\tname:    %%q,\n", path.Clean("/"+filepath.ToSlash(rel)))
		if ff.mode.IsDir() {
			fmt.Fprintln(f, "\t\tcontent: \"\",")
		} else {
			b, err := ioutil.ReadFile(ff.path)
			if err != nil {
				return err
			}
			fmt.Fprintf(f, "\t\tcontent: %%q,\n", string(b))
		}
		switch {
		case ff.mode.IsDir(): // directory
			fmt.Fprintln(f, "\t\tmode:    0755 | os.ModeDir,")
		case ff.mode&0100 != 0: // executable file
			fmt.Fprintln(f, "\t\tmode:    0755,")
		default:
			fmt.Fprintln(f, "\t\tmode:    0644,")
		}
		fmt.Fprintf(f, "\t\tnext:    %%d,\n", ff.next)
		if len(ff.children) > 0 {
			fmt.Fprintf(f, "\t\tchild:   %%d,\n", ff.children[0])
		} else {
			fmt.Fprint(f, "\t\tchild:   -1,\n")
		}
		fmt.Fprint(f, "\t},\n")
	}
	footer := %c%s%c
	fmt.Fprintln(f, footer)
	if err := f.Close(); err != nil {
		return err
	}

	f, err = os.OpenFile(filepath.Join(out, filename), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	format := %c%s%c
	fmt.Fprintf(f, format, 96, header, 96, 96, footer, 96, 96, format, 96)
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}
`
	fmt.Fprintf(f, format, 96, header, 96, 96, footer, 96, 96, format, 96)
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}
