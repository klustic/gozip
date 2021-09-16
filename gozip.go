package gozip

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"time"
)

// IsZip checks to see if path is already a zip file
func IsZip(path string) bool {
	r, err := zip.OpenReader(path)
	if err == nil {
		r.Close()
		return true
	}
	return false
}

// Zip takes all the files (dirs) and zips them into path
func Zip(path string, dirs []string) (err error) {
	if IsZip(path) {
		// return errors.New(path + " is already a zip file")
		return AppendZip(path, dirs)
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	startoffset, err := f.Seek(0, os.SEEK_END)
	if err != nil {
		return
	}

	w := zip.NewWriter(f)
	w.SetOffset(startoffset)

	for _, dir := range dirs {
		err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			fh, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			fh.Name = path

			p, err := w.CreateHeader(fh)
			if err != nil {
				return err
			}
			if !info.IsDir() {
				content, err := ioutil.ReadFile(path)
				if err != nil {
					return err
				}
				_, err = p.Write(content)
				if err != nil {
					return err
				}
			}
			return err
		})
	}
	err = w.Close()
	return
}

func generateChunk(name string, data []byte) (chunk []byte, err error) {
	chunk = make([]byte, 0, 0)

	// Calculate CRC32 of uncompressed data
	crc := make([]byte, 4, 4)
	binary.LittleEndian.PutUint32(crc, crc32.ChecksumIEEE(data))

	// Compress data (DEFLATE)
	var temp bytes.Buffer
	flateWriter, err := flate.NewWriter(&temp, flate.DefaultCompression)
	defer flateWriter.Close()
	flateWriter.Write(data)
	flateWriter.Flush()
	compressedData := temp.Bytes()

	// Calculate size of uncompressed data
	sizeU := make([]byte, 4, 4)
	binary.LittleEndian.PutUint32(sizeU, uint32(len(data)))

	// Calculate size of compressed data
	sizeC := make([]byte, 4, 4)
	binary.LittleEndian.PutUint32(sizeC, uint32(len(compressedData)))

	// Calculate name length
	sizeN := make([]byte, 2, 2)
	binary.LittleEndian.PutUint16(sizeN, uint16(len(name)))

	// Calculate modified timestamp in epoch time -- TODO : use actual modified time?
	now := time.Now()
	secs := uint32(now.Unix())
	extraTimestamp := []byte{0x55, 0x54, 5, 0, 1, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(extraTimestamp[5:9], secs)

	// Local file header
	chunk = append(chunk, []byte{'P', 'K', 3, 4}...)                // signature
	chunk = append(chunk, []byte{0x14, 00}...)                      // minimum version required
	chunk = append(chunk, []byte{0, 0}...)                          // general purpose bits
	chunk = append(chunk, []byte{8, 0}...)                          // compression method (DEFLATE)
	chunk = append(chunk, []byte{0, 0}...)                          // TODO : last modification time
	chunk = append(chunk, []byte{0, 0}...)                          // TODO : last modification date
	chunk = append(chunk, crc...)                                   // CRC32 of uncompressed data
	chunk = append(chunk, sizeC...)                                 // size of compressed data
	chunk = append(chunk, sizeU...)                                 // size of uncompressed data
	chunk = append(chunk, sizeN...)                                 // size of filename
	chunk = append(chunk, []byte{uint8(len(extraTimestamp)), 0}...) // size of extra field
	chunk = append(chunk, []byte(name)...)                          // filename
	chunk = append(chunk, extraTimestamp...)                        // extra data (timestamp)
	chunk = append(chunk, compressedData...)

	return
}

// Add hidden file(s) to ZIP
func AppendZip(path string, dirs []string) (err error) {
	// Open the ZIP file
	zipFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	stats, _ := zipFile.Stat()
	zipData := make([]byte, stats.Size(), stats.Size())
	_, err = zipFile.Read(zipData)

	// TODO : create byte slice of all files, serialized as ZIP files
	allData, _ := generateChunk("Hidden.xlsx", []byte("Kevin Was Here"))

	// Find end of central directory record, and offset to start of central directory
	var endOfCentralDirectoryPtr = -1
	ptr := len(zipData) - 4
	for {
		if bytes.Compare(zipData[ptr:ptr+4], []byte{'P', 'K', 5, 6}) == 0 {
			endOfCentralDirectoryPtr = ptr
			fmt.Printf("[+] Found EOCD Record: 0x%x\n", ptr)
			break
		}
		ptr -= 1
	}

	if endOfCentralDirectoryPtr < 0 {
		err = errors.New("Unable to find EOCD record")
		return
	}

	startOfCentralDirectory := uint32(binary.LittleEndian.Uint32(zipData[endOfCentralDirectoryPtr+16 : endOfCentralDirectoryPtr+20]))

	// Update start of central directory index
	newStartOfCentralDirectory := startOfCentralDirectory + uint32(len(allData))
	binary.LittleEndian.PutUint32(zipData[endOfCentralDirectoryPtr+16:endOfCentralDirectoryPtr+20], newStartOfCentralDirectory)

	// Move central directory and insert data
	newZipData := make([]byte, 0, 0)
	newZipData = append(newZipData, zipData[:startOfCentralDirectory]...)
	newZipData = append(newZipData, allData...)
	newZipData = append(newZipData, zipData[startOfCentralDirectory:]...)
	fmt.Println("[+] Injected data above the central directory!")

	// Write file back out
	err = zipFile.Truncate(int64(len(newZipData)))
	if err != nil {
		return err
	}
	zipFile.Seek(0, os.SEEK_SET)
	zipFile.Write(newZipData)
	zipFile.Sync()

	return nil
}

// Unzip unzips the file zippath and puts it in destination
func Unzip(zippath string, destination string) (err error) {
	r, err := zip.OpenReader(zippath)
	if err != nil {
		return err
	}
	for _, f := range r.File {
		fullname := path.Join(destination, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(fullname, f.FileInfo().Mode().Perm())
		} else {
			os.MkdirAll(filepath.Dir(fullname), 0755)
			perms := f.FileInfo().Mode().Perm()
			out, err := os.OpenFile(fullname, os.O_CREATE|os.O_RDWR, perms)
			if err != nil {
				return err
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			_, err = io.CopyN(out, rc, f.FileInfo().Size())
			if err != nil {
				return err
			}
			rc.Close()
			out.Close()

			mtime := f.FileInfo().ModTime()
			err = os.Chtimes(fullname, mtime, mtime)
			if err != nil {
				return err
			}
		}
	}
	return
}

// UnzipList Lists all the files in zip file
func UnzipList(path string) (list []string, err error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return
	}
	for _, f := range r.File {
		list = append(list, f.Name)
	}
	return
}
