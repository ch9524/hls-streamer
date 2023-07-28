package hls

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

// Version Indicates the package version
var Version = "1.0.0"

// ManifestTypes indicates the manifest type
type ManifestTypes int

const (
	// Vod Indicates VOD manifest
	Vod ManifestTypes = iota

	//LiveEvent Indicates a live manifest type event (always growing)
	LiveEvent

	//LiveWindow Indicates a live manifest type sliding window (fixed size)
	LiveWindow
)

// OutputTypes indicates the manifest type
type OutputTypes int

const (
	// HlsOutputModeNone No no write data
	HlsOutputModeNone OutputTypes = iota

	// HlsOutputModeFile Saves chunks to file
	HlsOutputModeFile

	// HlsOutputModeHTTP chunks to chunked streaming server
	HlsOutputModeHTTP
)

// Chunk Chunk information
type Chunk struct {
	IsGrowing bool
	FileName  string
	DurationS float64
	IsDisco   bool
}

// Hls Hls chunklist
type Hls struct {
	log                   *logrus.Logger
	manifestType          ManifestTypes
	version               int
	isIndependentSegments bool
	targetDurS            float64
	slidingWindowSize     int
	mseq                  int64
	dseq                  int64
	chunks                []Chunk
	chunklistFileName     string
	initChunkDataFileName string
	outputType            OutputTypes
	httpClient            *http.Client
	httpScheme            string
	httpHost              string

	isClosed bool
}

// New Creates a hls chunklist manifest
func New(
	log *logrus.Logger,
	ManifestType ManifestTypes,
	version int,
	isIndependentSegments bool,
	targetDurS float64,
	slidingWindowSize int,
	chunklistFileName string,
	initChunkDataFileName string,
	outputType OutputTypes,
	httpClient *http.Client,
	httpScheme string,
	httpHost string,
) Hls {
	h := Hls{
		log,
		ManifestType,
		version,
		isIndependentSegments,
		targetDurS,
		slidingWindowSize,
		0,
		0,
		make([]Chunk, 0),
		chunklistFileName,
		initChunkDataFileName,
		outputType,
		httpClient,
		httpScheme,
		httpHost,
		false,
	}

	return h
}

// SetInitChunk Adds a chunk init infomation
func (p *Hls) SetInitChunk(initChunkFileName string) {
	p.initChunkDataFileName = initChunkFileName
}

func (p *Hls) saveChunklist() error {
	ret := error(nil)

	hlsStrByte := []byte(p.String())

	if p.outputType == HlsOutputModeFile {
		ret = p.saveManifestToFile(hlsStrByte)
	} else if p.outputType == HlsOutputModeHTTP {
		ret = p.saveManifestToHTTP(hlsStrByte)
	}

	return ret
}

// CloseManifest Adds a chunk init infomation
func (p *Hls) CloseManifest(saveChunklist bool) error {
	ret := error(nil)

	p.isClosed = true

	if saveChunklist {
		ret = p.saveChunklist()
	}

	return ret
}

// SetHlsVersion Sets manifest version
func (p *Hls) SetHlsVersion(version int) {
	p.version = version
}

func (p *Hls) saveManifestToFile(manifestByte []byte) error {
	if p.chunklistFileName != "" {
		err := ioutil.WriteFile(p.chunklistFileName, manifestByte, 0644)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Hls) saveManifestToHTTP(manifestByte []byte) error {

	if p.chunklistFileName != "" {
		req := &http.Request{
			Method: "POST",
			URL: &url.URL{
				Scheme: p.httpScheme,
				Host:   p.httpHost,
				Path:   "/" + p.chunklistFileName,
			},
			ProtoMajor:    1,
			ProtoMinor:    1,
			ContentLength: -1,
			Body:          ioutil.NopCloser(bytes.NewReader(manifestByte)),
			Header:        http.Header{},
		}

		if strings.ToLower(path.Ext(p.chunklistFileName)) == ".m3u8" {
			req.Header.Set("Content-Type", "application/vnd.apple.mpegurl")
		}

		_, err := p.httpClient.Do(req)

		if err != nil {
			p.log.Error("Error uploading ", p.chunklistFileName, ". Error: ", err)
		} else {
			p.log.Debug("Upload of ", p.chunklistFileName, " complete")
		}
	}

	return nil
}

// AddChunk Adds a new chunk
func (p *Hls) AddChunk(chunkData Chunk, saveChunklist bool) error {
	ret := error(nil)

	p.chunks = append(p.chunks, chunkData)

	if p.manifestType == LiveWindow && len(p.chunks) > p.slidingWindowSize {
		//Remove first
		if p.chunks[0].IsDisco {

		}
		p.chunks = p.chunks[1:]
		p.mseq++
	}

	if saveChunklist {
		ret = p.saveChunklist()
	}

	return ret
}

// String write info to chunklist.m3u8
func (p *Hls) String() string {
	var buffer bytes.Buffer

	buffer.WriteString("#EXTM3U\n")
	buffer.WriteString("#EXT-X-VERSION:" + strconv.Itoa(p.version) + "\n")
	buffer.WriteString("#EXT-X-MEDIA-SEQUENCE:" + strconv.FormatInt(p.mseq, 10) + "\n")
	buffer.WriteString("#EXT-X-DISCONTINUITY-SEQUENCE:" + strconv.FormatInt(p.dseq, 10) + "\n")

	if p.manifestType == Vod {
		buffer.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")

	} else if p.manifestType == LiveEvent {
		buffer.WriteString("#EXT-X-PLAYLIST-TYPE:EVENT\n")
	}

	buffer.WriteString("#EXT-X-TARGETDURATION:" + fmt.Sprintf("%.0f", p.targetDurS) + "\n")

	if p.isIndependentSegments {
		buffer.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")
	}

	if p.initChunkDataFileName != "" {
		chunkPath, _ := filepath.Rel(path.Dir(p.chunklistFileName), p.initChunkDataFileName)
		buffer.WriteString("#EXT-X-MAP:URI=\"" + chunkPath + "\"\n")
	}

	for _, chunk := range p.chunks {
		if chunk.IsDisco {
			buffer.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		buffer.WriteString("#EXTINF:" + fmt.Sprintf("%.8f", chunk.DurationS) + ",\n")

		chunkPath, _ := filepath.Rel(path.Dir(p.chunklistFileName), chunk.FileName)
		buffer.WriteString(chunkPath + "\n")
	}

	if p.isClosed {
		buffer.WriteString("#EXT-X-ENDLIST\n")
	}

	return buffer.String()
}
