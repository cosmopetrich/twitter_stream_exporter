package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	envAccessToken    = "TWITTER_ACCESS_TOKEN"
	envAccessSecret   = "TWITTER_ACCESS_SECRET"
	envConsumerKey    = "TWITTER_CONSUMER_KEY"
	envConsumerSecret = "TWITTER_CONSUMER_SECRET"
)

var (
	// Version is set to the version of the exporter at build-time
	Version = "UNKNOWN"
	// BuildDate is set to the current date+time at build-time
	BuildDate = "UNKNOWN"
	// CommitSHA1 is set to the SHA of the git HEAD at build-time
	CommitSHA1 = "UNKNOWN"
)

// twitterConfig contains the arguments necessary to connect to the streaming API.
type twitterConfig struct {
	accessToken    string
	tokenSecret    string
	consumerKey    string
	consumerSecret string
	track          []string
}

// getTwitterClient does the oauth dance and returns a Twitter client.
func getTwitterClient(c twitterConfig) *twitter.Client {
	oc := oauth1.NewConfig(c.consumerKey, c.consumerSecret)
	ot := oauth1.NewToken(c.accessToken, c.tokenSecret)
	hc := oc.Client(oauth1.NoContext, ot)
	return twitter.NewClient(hc)
}

// Exporter collects metrics from the Twitter API.
type Exporter struct {
	stream   *twitter.Stream
	keywords map[string]bool

	matchingTweets *prometheus.CounterVec
	tagMentions    *prometheus.CounterVec
	userMentions   *prometheus.CounterVec
	wordMentions   *prometheus.CounterVec
}

// NewExporter returns an initialized Exporter.
func NewExporter(c twitterConfig) (*Exporter, error) {
	e := Exporter{}

	e.matchingTweets = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "twitter_stream_tweets_total",
		Help: "Total number of tweets delivered to the stream.",
	}, []string{"retweet"})
	e.tagMentions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "twitter_stream_hashtag_mentions_total",
		Help: "Total mentions of tracked keywords as hashtags.",
	}, []string{"keyword", "retweet"})
	e.userMentions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "twitter_stream_user_mentions_total",
		Help: "Total mentions of tracked keywords as usernames.",
	}, []string{"keyword", "retweet"})
	e.wordMentions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "twitter_stream_word_mentions_total",
		Help: "Total mentions of tracked keywords as raw words.",
	}, []string{"keyword", "retweet"})

	e.keywords = map[string]bool{}
	for _, s := range c.track {
		e.keywords[strings.ToLower(s)] = true
	}

	fp := &twitter.StreamFilterParams{
		Track:         c.track,
		StallWarnings: twitter.Bool(true),
	}

	s, err := getTwitterClient(c).Streams.Filter(fp)
	if err != nil {
		return nil, err
	}

	e.stream = s

	d := twitter.NewSwitchDemux()
	d.Tweet = e.parseTweet
	go d.HandleChan(e.stream.Messages)

	return &e, nil
}

// Collect implements the Prometheus collector interface.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.matchingTweets.Collect(ch)
	e.tagMentions.Collect(ch)
	e.userMentions.Collect(ch)
	e.wordMentions.Collect(ch)
}

// Describe implements the Prometheus collector interface.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	e.matchingTweets.Describe(ch)
	e.tagMentions.Describe(ch)
	e.userMentions.Describe(ch)
	e.wordMentions.Describe(ch)
}

// parseTweet reads a single tweet and increments the appropriate counters.
func (e *Exporter) parseTweet(t *twitter.Tweet) {
	var rt string
	var s *twitter.Tweet
	if t.RetweetedStatus != nil {
		rt = "true"
		s = t.RetweetedStatus
	} else {
		rt = "false"
		s = t
	}

	e.matchingTweets.WithLabelValues(rt).Inc()

	for _, h := range s.Entities.Hashtags {
		lh := strings.ToLower(h.Text)
		if e.keywords[lh] {
			e.tagMentions.WithLabelValues(lh, rt).Inc()
		}
	}
	for _, u := range s.Entities.UserMentions {
		lu := strings.ToLower(u.ScreenName)
		if e.keywords[lu] {
			e.userMentions.WithLabelValues(lu, rt).Inc()
		}
	}
	for _, w := range strings.Fields(strings.ToLower(s.Text)) {
		if e.keywords[w] {
			e.wordMentions.WithLabelValues(w, rt).Inc()
		}
	}
}

func main() {
	var (
		track         = flag.String("twitter.track", "", "Mandatory comma-separated list of keywords to track.")
		listenAddress = flag.String("web.listen-address", ":19000", "Address to listen on for web interface and telemetry.")
		metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
	)
	flag.Parse()
	if *track == "" {
		log.Fatalf("At least one keyword must be provided to -twitter.track")
	}

	c := twitterConfig{
		accessToken:    os.Getenv(envAccessToken),
		tokenSecret:    os.Getenv(envAccessSecret),
		consumerKey:    os.Getenv(envConsumerKey),
		consumerSecret: os.Getenv(envConsumerSecret),
		track:          strings.Split(*track, ","),
	}
	if c.accessToken == "" {
		log.Fatalf("No Twitter access token provided, please set %s", envAccessToken)
	}
	if c.tokenSecret == "" {
		log.Fatalf("No Twitter access token secret provided, please set %s", envAccessSecret)
	}
	if c.consumerKey == "" {
		log.Fatalf("No Twitter consumer key provided, please set %s", envConsumerKey)
	}
	if c.consumerSecret == "" {
		log.Fatalf("No Twitter consumer secret provided, please set %s", envConsumerSecret)
	}

	e, err := NewExporter(c)
	if err != nil {
		log.Fatal(err)
	}
	prometheus.MustRegister(e)

	bi := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "twitter_stream_exporter_build_info",
		Help: "twitter_stream exporter build info.",
	}, []string{"version", "commit_sha", "build_date", "golang_version"})
	prometheus.MustRegister(bi)
	bi.WithLabelValues(Version, CommitSHA1, BuildDate, runtime.Version()).Set(1)

	log.Printf("Starting twitter_stream_exporter %s (build date: %s) (sha1: %s)\n", Version, BuildDate, CommitSHA1)
	log.Printf("Metrics are avaiable at %s%s", *listenAddress, *metricsPath)

	http.Handle(*metricsPath, promhttp.Handler())
	s := &http.Server{Addr: *listenAddress}
	go func() {
		log.Print(s.ListenAndServe())
	}()

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Println("Shutting down")
	e.stream.Stop()
	s.Close()
}
