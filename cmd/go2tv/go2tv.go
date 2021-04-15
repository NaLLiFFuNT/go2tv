package main

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"

	"github.com/alexballas/go2tv/internal/httphandlers"
	"github.com/alexballas/go2tv/internal/interactive"
	"github.com/alexballas/go2tv/internal/iptools"
	"github.com/alexballas/go2tv/internal/soapcalls"
	"github.com/koron/go-ssdp"
)

var (
	version       string
	build         string
	dmrURL        string
	serverStarted = make(chan struct{})
	devices       = make(map[int][]string)
	videoArg      = flag.String("v", "", "Path to the video file.")
	subsArg       = flag.String("s", "", "Path to the subtitles file.")
	listPtr       = flag.Bool("l", false, "List all available UPnP/DLNA MediaRenderer models and URLs.")
	targetPtr     = flag.String("t", "", "Cast to a specific UPnP/DLNA MediaRenderer URL.")
	versionPtr    = flag.Bool("version", false, "Print version.")
)

func main() {
	flag.Parse()

	exit, err := checkflags()
	check(err)

	if exit {
		os.Exit(0)
	}

	absVideoFile, err := filepath.Abs(*videoArg)
	check(err)

	absSubtitlesFile, err := filepath.Abs(*subsArg)
	check(err)

	transportURL, controlURL, err := soapcalls.DMRextractor(dmrURL)
	check(err)

	whereToListen, err := iptools.URLtoListenIPandPort(dmrURL)
	check(err)

	newsc, err := interactive.InitNewScreen()
	check(err)

	// The String() method of the net/url package will properly escape the URL
	// compared to the url.QueryEscape() method.
	videoFileURLencoded := &url.URL{Path: filepath.Base(absVideoFile)}
	subsFileURLencoded := &url.URL{Path: filepath.Base(absSubtitlesFile)}

	tvdata := &soapcalls.TVPayload{
		TransportURL: transportURL,
		ControlURL:   controlURL,
		CallbackURL:  "http://" + whereToListen + "/callback",
		VideoURL:     "http://" + whereToListen + "/" + videoFileURLencoded.String(),
		SubtitlesURL: "http://" + whereToListen + "/" + subsFileURLencoded.String(),
	}

	s := httphandlers.NewServer(whereToListen)

	// We pass the tvdata here as we need the callback handlers to be able to react
	// to the different media renderer states.
	go func() {
		s.ServeFiles(serverStarted, absVideoFile, absSubtitlesFile, &httphandlers.HTTPPayload{Soapcalls: tvdata, Screen: newsc})
	}()
	// Wait for HTTP server to properly initialize
	<-serverStarted

	err = tvdata.SendtoTV("Play1")
	check(err)

	newsc.InterInit(*tvdata)
}

func loadSSDPservices() error {
	list, err := ssdp.Search(ssdp.All, 1, "")
	if err != nil {
		return err
	}

	counter := 0
	for _, srv := range list {
		// We only care about the AVTransport services for basic actions
		// (stop,play,pause). If we need support other functionalities
		// like volume control we need to use the RenderingControl service.
		if srv.Type == "urn:schemas-upnp-org:service:AVTransport:1" {
			curr := []string{srv.Server, srv.Location}
			add := true
			for _, z := range devices {
				if z[0] == curr[0] && z[1] == curr[1] {
					add = false
					break
				}
			}
			if add {
				counter++
				devices[counter] = curr
			}
		}
	}
	if counter > 0 {
		return nil
	}

	return errors.New("loadSSDPservices: No available Media Renderers")
}

func devicePicker(i int) (string, error) {
	if i > len(devices) || len(devices) == 0 || i <= 0 {
		return "", errors.New("devicePicker: Requested device not available")
	}
	keys := make([]int, 0)
	for k := range devices {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, k := range keys {
		if i == k {
			return devices[k][1], nil
		}
	}
	return "", errors.New("devicePicker: Something went terribly wrong")
}

func check(err error) {
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Encountered error(s): %s\n", err)
		os.Exit(1)
	}
}