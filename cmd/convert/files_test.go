package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The writer also gains its sync, which maxon's has not.
func TestConvertFileEndingsAreDestructors(t *testing.T) {
	in := "type FileWriter\n\tlet file as __ManagedFile\n\n\tfunction write(content ByteArray)\n\t\tfile.write(content.managed)\n\tend 'write'\n\n\tfunction close()\n\t\tfile.close()\n\tend 'close'\n\n\tfunction writeAll(content ByteArray)\n\t\twrite(content)\n\t\tclose()\n\tend 'writeAll'\nend 'FileWriter'\n"
	want := "type FileWriter\n\tlet file as __ManagedFile\n\n\tfunction write(content ByteArray)\n\t\tfile.write(content.managed)\n\tend 'write'\n\n\tfunction close(destructs self) throws FileWriteError\n\t\ttry file.close() otherwise (e) 'unclosed'\n\t\t\tthrow writeFailureOf(e)\n\t\tend 'unclosed'\n\tend 'close'\n\n\tfunction writeAll(destructs self, content ByteArray)\n\t\twrite(content)\n\t\tclose()\n\tend 'writeAll'\n\n\t/// Force the file's data to its device and throw `FileWriteError.failed` where the system refuses or fails it, as Go's `File.Sync` does.\n\tpublic function sync(mutates self) throws FileWriteError\n\t\ttry file.sync() otherwise (e) 'unsynced'\n\t\t\tthrow writeFailureOf(e)\n\t\tend 'unsynced'\n\tend 'sync'\nend 'FileWriter'\n"
	if got := Convert("test.maxon", in, sourceFile).Text; got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestConvertFileBuffersAreBytes(t *testing.T) {
	in := "function read(file __ManagedFile, most Count, managed __ManagedMemory) returns Count\n\tlet buffer = try __ManagedMemory.create(most, 1) otherwise 'made'\n\t\treturn 0\n\tend 'made'\n\treturn try file.readAppending(buffer, most) otherwise 0\nend 'read'\n"
	got := Convert("test.maxon", in, sourceFile).Text
	for _, want := range []string{"managed __ManagedMemory with Byte)", "try ByteMemory.create(most, 1)", "file.readAppending(buffer, most: most)"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
}

func TestConvertBareBufferOutsideAFileIsLeft(t *testing.T) {
	in := "function length(managed __ManagedMemory) returns Count\n\treturn 0\nend 'length'\n"
	if got := Convert("test.maxon", in, sourceFile).Text; strings.Contains(got, "with Byte") {
		t.Errorf("a buffer of no file became one of bytes:\n%s", got)
	}
}

// The alias that rewriteFileBuffers writes a create through is the one the library declares beside __ManagedFile.
func TestFileBuffersAliasIsTheLibrarys(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "stdlib", "file", "managedFile.maxon"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "typealias "+fileBuffersAlias+" = __ManagedMemory with Byte") {
		t.Errorf("stdlib/file/managedFile.maxon declares no typealias %s of a buffer of bytes", fileBuffersAlias)
	}
}

func TestConvertFileWriterHelperStaysPublic(t *testing.T) {
	in := "public type FileWriter\n\tpublic static function open() returns Count\n\t\treturn 0\n\tend 'open'\nend 'FileWriter'\n\npublic type Other\n\tpublic static function make() returns Count\n\t\treturn 0\n\tend 'make'\nend 'Other'\n"
	want := "public type FileWriter\n\tpublic static function open() returns Count\n\t\treturn 0\n\tend 'open'\nend 'FileWriter'\n\nexport type Other\n\texport static function make() returns Count\n\t\treturn 0\n\tend 'make'\nend 'Other'\n"
	if got := Convert("maxon/stdlib/helpers/file/fileWriter.maxon", in, sourceFile).Text; got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestConvertOrphanedMemberOfABuiltinRecordIsDropped(t *testing.T) {
	in := "public type File\n\tpublic static function readText(path FilePath) returns String\n\t\treturn whole(path)\n\tend 'readText'\n\n\tpublic static function openRead(path FilePath) returns Count\n\t\treturn kept(path)\n\tend 'openRead'\n\n\tstatic function whole(path FilePath) returns String\n\t\treturn \"\"\n\tend 'whole'\n\n\tstatic function kept(path FilePath) returns Count\n\t\treturn 0\n\tend 'kept'\nend 'File'\n"
	got := Convert("maxon/stdlib/File.maxon", in, sourceFile).Text
	if strings.Contains(got, "function whole") || strings.Contains(got, "function readText") || !strings.Contains(got, "function kept") || !strings.Contains(got, "extension File") {
		t.Errorf("want an extension of File without readText and whole, with kept:\n%s", got)
	}
}
