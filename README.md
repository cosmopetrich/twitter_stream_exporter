# twitter_stream_exporter

Why waste time writing meaningful instrumentation like a sucker when you can make your users do your
monitoring for you? They say that [the best alerting is done from the persepctive of the user](https://docs.google.com/document/d/199PqyG3UsyXlwieHaqbGiWVa8eMWi8zzAn0YfcApr8Q/),
so let's harness the fact that people are going to jump on Twitter to complain the moment something
goes wrong.

## Building

```bash
export GOOS="linux"
export GOARCH="amd64"

VERSION=$(git describe --always --dirty)
SHA1=$(git rev-parse --short --verify HEAD)
BUILD_DATE=$(date -u +%F-%T)

go build -ldflags "-extldflags -static -X main.VERSION=${VERSION} -X main.COMMIT_SHA1=${SHA1} -X main.BUILD_DATE=${BUILD_DATE}"
```

## Usage

You'll need to [Generate a twitter access token pair](https://dev.twitter.com/oauth/overview/application-owner-access-tokens).
The exporter doesn't need to post to Twitter, so you should set the newly created application's 
permissions model to "Read only".

Once that's done, export the four keys as environment variables.

```bash
export TWITTER_ACCESS_TOKEN="..."
export TWITTER_ACCESS_SECRET="..."
export TWITTER_CONSUMER_KEY="..."
export TWITTER_CONSUMER_SECRET="..."
```

Then run the exporter.

```bash
twitter_stream_exporter -twitter.track 'akeyword,anotherkeyword'
```

The value provided to `-twitter.track` should be a comma-separated list of phrases to use in filtering
tweets. See [Twitter's API documentation](https://dev.twitter.com/streaming/overview/request-parameters#track)
for details on supported syntax, and continue reading for caveats.


## Exported metrics

The exporter provides a set of counters that can be used to determine how frequently keywords are
being used.

| Metric | Notes |
| ------ | ----- |
| twitter_stream_tweets_total | The total number of tweets delivered to the stream. |
| twitter_stream_user_mentions_total | The number of times a username provided as an argument to `-twitter.track` has been @mentioned in the text of a tweet. |
| twitter_stream_hashtag_mentions_total | Then number of times a hashtag provided as an argument to `-twitter.track` has been #mentioned in the text of a tweet. |
| twitter_stream_word_mentions_total | The number of times an arguent to `-twitter.track` has been mentioned as a raw keyword (not an @mention or #hashtag) in the text of a tweet. |

All metrics have a `retweet` label (`true` or `false`). The `*_mentions_total` metrics also have a
`keyword` label. Keywords are normalised to lowercase.

It's possible for the sum of the `*_mentions_total` metrics to exceed `twitter_stream_tweets_total`
if individual tweets contain multiple keywords or keywords used in multiple contexts. The value of
`twitter_stream_tweets_total` may exceed the sum of the other matreics when twitter reutrns tweets
using filtering logic this exporter does not replicate.

A full sample of output can be found below.

```
twitter_stream_hashtag_mentions_total{keyword="widgetfrobber",retweet="false"} 11
twitter_stream_hashtag_mentions_total{keyword="widgetfrobber",retweet="true"} 23
twitter_stream_hashtag_mentions_total{keyword="dodgycorp",retweet="false"} 7
twitter_stream_hashtag_mentions_total{keyword="dodgycorp",retweet="true"} 14
twitter_stream_tweets_total{retweet="false"} 89
twitter_stream_tweets_total{retweet="true"} 71
twitter_stream_user_mentions_total{keyword="dodgycorp",retweet="true"} 2
twitter_stream_word_mentions_total{keyword="widgetfrobber",retweet="false"} 29
twitter_stream_word_mentions_total{keyword="widgetfrobber",retweet="true"} 19
twitter_stream_word_mentions_total{keyword="dodgycorp",retweet="false"} 37
twitter_stream_word_mentions_total{keyword="dodgycorp",retweet="true"} 11
```

## Caveats

This exporter uses the Twitter streaming API. The streaming API returns a much more complete set of
data than the regular search API, and the last thing we'd want to do is make ill-informed decisions.
The streaming API brings some caveats with it.

 * As of 2017-04 each account is [limited to a single active stream](https://dev.twitter.com/streaming/public)
with [up to 400 track keywords](https://dev.twitter.com/streaming/reference/post/statuses/filter).

 * There's a [small chance that the streaming API may return duplicate messages](https://dev.twitter.com/streaming/overview/processing),
and the exporter makes no attempt to account for this.

 * The underlying Golang Twitter stream handler [does not support gzip](https://github.com/dghubble/go-twitter#roadmap).
 
The streaming API doesn't provide any indication as to which filter caused it to deliver a Tweet,
meaning the exporter needs to inspect each message it receives in order to set meaningful labels.
The exporter doesn't implement any of the [fuzzier matching that Twitter does](https://dev.twitter.com/streaming/overview/request-parameters#track)
and cannot match `track` arguments other than when they appear as single words. The following
circumstances in which it can't detect keywords are known to arise.

 * Immediately alongside UTF characters (`KEYWORDベ`).
 * When separated with an underscore (`KEYWORD_somethingelse`), and possibly other characters.
 * When adjacent to punctuation (`"KEYWORD"`).
 * Hashtags (and possibly @mentions and bare words?) on the other side of t.co direct links.

There are some odd occasions in which the stream also appears to return some tweets that seemingly
match none of the filters. That may be an expected behaviour of the streaming API, or some less
obvious filtering behaviour.
