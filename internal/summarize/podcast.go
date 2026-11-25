package summarize

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Turn is one host's line in the podcast script.
type Turn struct {
	Host string `json:"host"`
	Text string `json:"text"`
}

// wordsPerMinute is a relaxed conversational pace.
const wordsPerMinute = 150

const podcastSystem = `You write the script for a two-host audio version of a personal weekly market summary. The listener hears it while exercising, so it has to make sense by ear alone, with nothing to look at.

Both hosts are equally informed co-hosts, not an interviewer and an expert. Split the stories between them: each host brings in and explains about half of the stories, and the other reacts, asks the question a curious listener would ask, adds a fact, or connects it to another story. About one turn in four is a short question or reaction that sets up the next fact; the rest carry the news. The first host opens and closes the episode. Neither host only asks questions or only agrees. No time-of-day greetings: the episode may be heard at any hour.

The rhythm, shown with invented content (never reuse it):
A: A big retailer named a new CEO this week, and it's someone from inside.
B: Who's taking over?
A: The finance chief, who has been there for years. The outgoing CEO stays on as chair.
B: That's the orderly version of a handoff. Compare it with the chipmaker story, where the board reversed itself within ten days.
A: Right, and that one is still under investigation.

Rules:
- Use only the facts in the brief. Never add figures, dates, causes, stock price moves or market reactions it does not state. Explaining a general idea (what short interest means, why bond yields matter) is fine; every claim about this week must come from the brief.
- Length: aim for the word count given; most weeks have enough material to fill it with substance (more context on the big stories, how they connect). If the week is truly thin, be shorter rather than pad or repeat.
- Choose, don't recite. Spend the time on the stories that matter and say why they matter. Lists in the brief get a sentence or two each, naming only the top items: the season-so-far figures (one or two numbers, not every sector), insider trades (the three to five largest or most unusual, then "several others sold under pre-arranged plans"), and short interest (the flagged stocks only).
- Never mention the brief, your sources, "the summary", or what they leave out; the listener does not know they exist. When no reason is given for something, either say the company gave no reason or say nothing about reasons.
- Order: a short welcome that says which week this covers; the biggest stories first (major company news, notable market or economic moves, the Fed); then earnings and calls grouped by sector or theme; then insider trades and short interest briefly; finish with what is coming up next week and a quick sign-off.
- Write for the ear. Round numbers to two or three significant figures and say them the way people talk: "about 3.8 percent", "roughly 112 million dollars", "about 1.64 billion dollars" (never "1,638.1 million"), "up about a quarter of a percent". Give a range as its midpoint unless the range itself is the news. At most two numbers per sentence. Use words, not symbols: no %, $, +, /, &. Say company names, not tickers ("Nvidia", not "NVDA"). Say dates as weekdays or "December 4th", not 2026-12-04. Common spoken abbreviations (CEO, GDP, the Fed, AI) are fine; spell out the rest.
- Index moves: give the percent change, and for the main stock indexes also say what the sample amount put in a fund tracking them would now be worth, using the dollar figures as given ("the S and P 500 fell about 1.4 percent, so ten thousand dollars in a fund tracking it would now be worth about 9,860 dollars"). Never give an index move in points alone; points only mean something next to the percent. Use the dollar translation for the two or three biggest index moves, not every row.
- Explain each market term briefly the first time, for a listener who is learning how markets work (basis points, guidance, diluted earnings per share, short interest, days to cover, a 13D filing, a 10b5-1 plan).
- Keep turns to one to four sentences. Short reactions are fine when they add something ("That's a lot of stock for one person to sell."), but no filler: no "great question", no laughing, no sound effects, no stage directions, no text in brackets or parentheses.
- Hosts may address each other by name now and then, not every turn.
- No investment advice, no hype, no predictions about where any stock or the market will go.
- Do not guess what the listener is doing (no "enjoy your run").
- The text is read aloud by a speech engine exactly as written: plain sentences only. No markdown, lists, emojis, or speaker names at the start of a line.`

func podcastSchema(hosts [2]string) json.RawMessage {
	names, _ := json.Marshal(hosts[:])
	return json.RawMessage(fmt.Sprintf(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["turns"],
  "properties": {
    "turns": {"type": "array", "items": {
      "type": "object",
      "additionalProperties": false,
      "required": ["host","text"],
      "properties": {"host": {"type": "string", "enum": %s}, "text": {"type": "string"}}
    }}
  }
}`, names))
}

// Podcast writes a two-host conversation from the plain-text brief. hosts[0]
// leads.
func (s *Summarizer) Podcast(ctx context.Context, brief string, hosts [2]string, minutes int) ([]Turn, error) {
	user := fmt.Sprintf("Hosts: %s (leads) and %s.\nTarget length: about %d words (%d minutes).\n\n<brief>\n%s\n</brief>",
		hosts[0], hosts[1], minutes*wordsPerMinute, minutes, brief)
	out, err := s.call(ctx, Request{
		Model: cmp.Or(s.PodcastModel, s.OverviewModel), Effort: s.Effort, System: podcastSystem, User: user, Schema: podcastSchema(hosts),
		MaxTokens: 48000, // a long script, plus the model's thinking
	})
	if err != nil {
		return nil, err
	}
	var script struct {
		Turns []Turn `json:"turns"`
	}
	if err := json.Unmarshal([]byte(out), &script); err != nil {
		return nil, fmt.Errorf("parse podcast script: %w", err)
	}
	var turns []Turn
	for _, t := range script.Turns {
		if t.Text = strings.TrimSpace(t.Text); t.Text != "" {
			turns = append(turns, t)
		}
	}
	if len(turns) == 0 {
		return nil, errors.New("podcast script is empty")
	}
	return turns, nil
}
