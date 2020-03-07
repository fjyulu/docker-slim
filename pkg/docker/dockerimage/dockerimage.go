package dockerimage

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	manifestFileName = "manifest.json"
	layerSuffix      = "/layer.tar"
	topObjectMax     = 10
)

type Package struct {
	Manifest        *ManifestObject
	Config          *ConfigObject
	Layers          []*Layer
	LayerIDRefs     map[string]*Layer
	LayerRootFSRefs map[string]*Layer
}

type Layer struct {
	ID         string
	Index      int
	Path       string
	Stats      LayerStats
	Changes    Changeset
	Objects    []*ObjectMetadata
	References map[string]*ObjectMetadata
	Top        TopObjects
}

type LayerStats struct {
	BlobSize         uint64
	AllSize          uint64
	ObjectCount      uint64
	DirCount         uint64
	FileCount        uint64
	LinkCount        uint64
	MaxFileSize      uint64
	MaxDirSize       uint64
	DeletedCount     uint64
	DeletedDirCount  uint64
	DeletedFileCount uint64
	DeletedLinkCount uint64
	DeletedSize      uint64
	AddedSize        uint64
	ModifiedSize     uint64
}

type ChangeType int

const (
	ChangeUnknown = iota
	ChangeDelete
	ChangeAdd
	ChangeModify
)

type ObjectMetadata struct {
	Change     ChangeType
	Name       string
	Size       int64
	Mode       os.FileMode
	UID        int
	GID        int
	ModTime    time.Time
	ChangeTime time.Time
	LinkTarget string
}

type Changeset struct {
	Deleted  []int
	Added    []int
	Modified []int
}

func newPackage() *Package {
	pkg := Package{
		LayerIDRefs:     map[string]*Layer{},
		LayerRootFSRefs: map[string]*Layer{},
	}

	return &pkg
}

func newLayer(id string) *Layer {
	layer := Layer{
		ID:         id,
		Index:      -1,
		References: map[string]*ObjectMetadata{},
		Top:        NewTopObjects(topObjectMax),
	}

	return &layer
}

func LoadPackage(archivePath, imageID string, skipObjects bool) (*Package, error) {
	configObjectFileName := fmt.Sprintf("%s.json", imageID)
	afile, err := os.Open(archivePath)
	if err != nil {
		log.Errorf("dockerimage.LoadPackage: os.Open error - %v", err)
		return nil, err
	}

	defer afile.Close()

	pkg := newPackage()
	layers := map[string]*Layer{}

	tr := tar.NewReader(afile)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			log.Errorf("dockerimage.LoadPackage: error reading archive(%v) - %v", archivePath, err)
			return nil, err
		}

		if hdr == nil || hdr.Name == "" {
			log.Debugf("dockerimage.LoadPackage: ignoring bad tar header")
			continue
		}

		hdr.Name = filepath.Clean(hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeReg, tar.TypeSymlink:
			switch {
			case hdr.Name == manifestFileName:
				var manifests []ManifestObject
				if err := jsonFromStream(tr, &manifests); err != nil {
					log.Errorf("dockerimage.LoadPackage: error reading manifest file from archive(%v/%v) - %v", archivePath, manifestFileName, err)
					return nil, err
				}

				if len(manifests) == 0 {
					return nil, fmt.Errorf("dockerimage.LoadPackage: malformed manifest file - no manifests")
				}

				for _, m := range manifests {
					if m.Config == configObjectFileName {
						manifest := m
						pkg.Manifest = &manifest
						break
					}
				}

			case hdr.Name == configObjectFileName:
				var imageConfig ConfigObject
				if err := jsonFromStream(tr, &imageConfig); err != nil {
					log.Errorf("dockerimage.LoadPackage: error reading config object from archive(%v/%v) - %v", archivePath, configObjectFileName, err)
					return nil, err
				}

				pkg.Config = &imageConfig
			case strings.HasSuffix(hdr.Name, layerSuffix):
				parts := strings.Split(hdr.Name, "/")
				layerID := parts[0]
				layer, err := layerFromStream(tar.NewReader(tr), layerID)
				if err != nil {
					log.Errorf("dockerimage.LoadPackage: error reading layer from archive(%v/%v) - %v", archivePath, hdr.Name, err)
					return nil, err
				}

				layer.Path = hdr.Name
				layers[layerID] = layer
			}
		}
	}

	if pkg.Manifest == nil {
		return nil, fmt.Errorf("dockerimage.LoadPackage: missing manifest object for image ID - %v", imageID)
	}

	if pkg.Config == nil {
		return nil, fmt.Errorf("dockerimage.LoadPackage: missing image config object for image ID - %v", imageID)
	}

	for idx, layerPath := range pkg.Manifest.Layers {
		parts := strings.Split(layerPath, "/")
		layerID := parts[0]
		layer, ok := layers[layerID]
		if !ok {
			log.Errorf("dockerimage.LoadPackage: error missing layer in archive(%v/%v) - %v", archivePath, layerPath, err)
			return nil, fmt.Errorf("dockerimage.LoadPackage: missing layer (%v) for image ID - %v", layerPath, imageID)
		}

		layer.Index = idx
		pkg.Layers = append(pkg.Layers, layer)
		if len(pkg.Layers)-1 != layer.Index {
			return nil, fmt.Errorf("dockerimage.LoadPackage: layer index mismatch - %v / %v", len(pkg.Layers)-1, layer.Index)
		}

		if layerPath != layer.Path {
			return nil, fmt.Errorf("dockerimage.LoadPackage: layer path mismatch - %v / %v", layerPath, layer.Path)
		}

		if idx == 0 {
			for oidx, object := range layer.Objects {
				if object.Change == ChangeUnknown {
					object.Change = ChangeAdd
					layer.Changes.Added = append(layer.Changes.Added, oidx)
					layer.Stats.AddedSize += uint64(object.Size)
				}
			}
		} else {
			for oidx, object := range layer.Objects {
				if object.Change == ChangeUnknown {
					for prevIdx := 0; prevIdx < idx; prevIdx++ {
						prevLayer := pkg.Layers[prevIdx]
						if _, ok := prevLayer.References[object.Name]; ok {
							object.Change = ChangeModify
							layer.Changes.Modified = append(layer.Changes.Modified, oidx)
							layer.Stats.ModifiedSize += uint64(object.Size)

							break
						}
					}

					if object.Change == ChangeUnknown {
						object.Change = ChangeAdd
						layer.Changes.Added = append(layer.Changes.Added, oidx)
						layer.Stats.AddedSize += uint64(object.Size)
					}
				}
			}
		}

		pkg.LayerIDRefs[layerID] = layer
		//pkg.LayerRootFSRefs[diffID] = layer
	}

	return pkg, nil
}

func layerFromStream(tr *tar.Reader, layerID string) (*Layer, error) {
	layer := newLayer(layerID)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			log.Errorf("layerFromStream: error reading layer(%v) - %v", layerID, err)
			return nil, err
		}

		if hdr == nil || hdr.Name == "" {
			log.Debugf("layerFromStream: ignoring bad tar header")
			continue
		}

		hdr.Name = filepath.Clean(hdr.Name)

		object := &ObjectMetadata{
			Name:       hdr.Name,
			Size:       hdr.Size,
			Mode:       hdr.FileInfo().Mode(),
			UID:        hdr.Uid,
			GID:        hdr.Gid,
			ModTime:    hdr.ModTime,
			ChangeTime: hdr.ChangeTime,
		}

		layer.Objects = append(layer.Objects, object)
		layer.References[object.Name] = object

		layer.Stats.AllSize += uint64(object.Size)
		layer.Stats.ObjectCount++

		normalized, isDeleted, err := NormalizeFileObjectLayerPath(object.Name)
		if err == nil {
			object.Name = normalized
		}
		if isDeleted {
			object.Change = ChangeDelete
			idx := len(layer.Objects) - 1
			layer.Changes.Deleted = append(layer.Changes.Deleted, idx)
			layer.Stats.DeletedCount++
			layer.Stats.DeletedSize += uint64(object.Size)
			//NOTE:
			//This is not the real deleted size.
			//Need to find the actual object in a previous layer to know the actual size.
		}

		switch hdr.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			object.LinkTarget = hdr.Linkname
			layer.Stats.LinkCount++
			if isDeleted {
				layer.Stats.DeletedLinkCount++
			}
		case tar.TypeReg:
			layer.Stats.FileCount++
			if uint64(object.Size) > layer.Stats.MaxFileSize {
				layer.Stats.MaxFileSize = uint64(object.Size)
			}

			if isDeleted {
				layer.Stats.DeletedFileCount++
			}
		case tar.TypeDir:
			layer.Stats.DirCount++
			if uint64(object.Size) > layer.Stats.MaxDirSize {
				layer.Stats.MaxDirSize = uint64(object.Size)
			}

			if isDeleted {
				layer.Stats.DeletedDirCount++
			}
		}
	}

	return layer, nil
}

func jsonFromStream(reader io.Reader, data interface{}) error {
	return json.NewDecoder(reader).Decode(data)
}

type TarReadCloser struct {
	io.Reader
	io.Closer
}

func FileReaderFromTar(tarPath, filePath string) (io.ReadCloser, error) {
	tfile, err := os.Open(tarPath)
	if err != nil {
		log.Errorf("dockerimage.FileReaderFromTar: os.Open error - %v", err)
		return nil, err
	}

	defer tfile.Close()
	tr := tar.NewReader(tfile)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}

		if hdr == nil || hdr.Name == "" {
			continue
		}

		hdr.Name = filepath.Clean(hdr.Name)
		if hdr.Name == filePath {
			switch hdr.Typeflag {
			case tar.TypeReg, tar.TypeSymlink, tar.TypeLink:
				return TarReadCloser{
					Reader: tr,
					Closer: tfile,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no file - %s", filePath)
}

func FileDataFromTar(tarPath, filePath string) ([]byte, error) {
	tfile, err := os.Open(tarPath)
	if err != nil {
		log.Errorf("dockerimage.FileDataFromTar: os.Open error - %v", err)
		return nil, err
	}

	defer tfile.Close()
	tr := tar.NewReader(tfile)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}

		if hdr == nil || hdr.Name == "" {
			continue
		}

		hdr.Name = filepath.Clean(hdr.Name)
		if hdr.Name == filePath {
			switch hdr.Typeflag {
			case tar.TypeReg, tar.TypeSymlink, tar.TypeLink:
				return ioutil.ReadAll(tr)
			}
		}
	}

	return nil, fmt.Errorf("no file - %s", filePath)
}

func LoadManifestObject(archivePath, imageID string) (*ManifestObject, error) {
	return nil, nil
}

func LoadConfigObject(archivePath, imageID string) (*ConfigObject, error) {
	return nil, nil
}

func LoadLayer(archivePath, imageID, layerID string) (*Layer, error) {
	return nil, nil
}
