// Module for reading load information from trulos.com

package boards

import "fmt"
import "bytes"
import "log"
import "regexp"
import "net/url"
import "strings"
import "strconv"
import "time"
import "code.google.com/p/go.net/html"
import "code.google.com/p/go.net/html/atom"

import "scraper"

// TODO(jmacd): This list is not right; they sometimes list multiple
// and/or irregular equipment types, it seems.  "Reefer with Pallet
// exchange", "Flatbed with Sides", "Van Hazmat", ...
var equipmentTypes = []string{
	// "Double Drop",
	// "Flatbed / Step Deck",
	// "Flatbed with Tarps",
	// "Flatbed",
	// "Power Only",
	// "Reefer",
	// "Step Deck Removeable Gooseneck",
	// "Step Deck",
	// "Van / Reefer",
	"Van",
}

const (
	baseUri = "/Trulos/Post-Truck-Loads/Truck-Load-Board.aspx"
	contentId = "ContentPlaceHolder1_GridView1"
	// TODO(jmacd): It's escaped in the raw HTML! 
	pageRegexp = "javascript:__doPostBack\\('[a-zA-Z0-9$]+','Page$\\d+'\\)"
)

type trulosBoard struct {
	host  string
	stateRe   *regexp.Regexp
	pageRe  *regexp.Regexp
	states []*trulosState
}

type trulosState struct {
	board *trulosBoard
	name string
	uri string
}

type trulosScrape struct {
	state *trulosState
	equip string
	body  []byte
	actions [][]byte
	xml   []byte
	post  chan<- scraper.Scrape  
}

func NewTrulos() (LoadBoard, error) {
	stateRe, err := regexp.Compile(
		regexp.QuoteMeta(baseUri + "?STATE=") + "(\\w+)")
	if err != nil {
		log.Print("Invalid Trulos State regexp")
		return nil, err
	}
	pageRe, err := regexp.Compile(pageRegexp)
	if err != nil {
		log.Print("Invalid Trulos Page regexp")
		return nil, err
	}
	return &trulosBoard{ "www.trulos.com", stateRe, pageRe, nil }, nil
}

func (t *trulosBoard) Init() error {
	body, err := ReadUrl(t.host, "", "")
	if err != nil {
		return err
	}
	links := t.stateRe.FindAllStringSubmatch(string(body), -1)
	for _, si  := range links {
		t.states = append(t.states, &trulosState{t, si[1], si[0]})
	}
	return nil
}

func (s *trulosState) queryForEquip(equip string) string {
	return "?STATE=" + s.name + "&Equipment=" + url.QueryEscape(equip)
}

// Read asynchronously reads pages from the board and passes them to the
// scrape-evaluator.
func (t *trulosBoard) Read(ch chan<- scraper.Scrape) {
	post := make(chan scraper.Scrape)
	go ProcessScrapes(post)
	for _, state := range t.states {
		if state.name != "TX" {
			continue  // Test multi-page results!!!
		}
		for _, equip := range equipmentTypes {
			log.Println("Reading Trulos state", state.name, equip)
			query := state.queryForEquip(equip)
			body, err := ReadUrl(t.host, baseUri, query)
			if err != nil {
				log.Print("Problem reading Trulos", query)
			}
			fmt.Println("Raw HTML is\n", string(body))
			actions := state.board.pageRe.FindAll(
				body, -1)
			log.Println("Found actions:", actions)
			ch <- &trulosScrape{ state, equip, body, 
				actions, nil, post }
			// !!! Just one for now
			return
		}
	}
}

func ProcessScrapes(post <-chan scraper.Scrape) {
	for scrape := range post {
		scrape.(*trulosScrape).Process()
	}
}

func (s *trulosScrape) Id() string {
	return fmt.Sprint("Trulos-", s.state.name, "-", s.equip)
}

func (s *trulosScrape) Body() []byte {
	return s.body
}

func (s *trulosScrape) Actions() [][]byte {
	return s.actions
}

func (s *trulosScrape) Scraped(xml []byte, err error) {
	if err != nil {
		log.Print("Scrape error: ", s.state, ": ", s.equip)
	} else {
		s.xml = xml
		s.post <- s
	}
}

func (s *trulosScrape) Process() {
	doc, err := html.Parse(bytes.NewReader(s.xml))
	if err != nil {
		log.Print("Scrape parse error", s.state.name, s.equip, err)
		return
	}
	s.TraverseHTML(doc)
}

func attrIdIs(n *html.Node, value string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "id" && attr.Val == value {
			return true
		}
	}
	return false
}

func (s *trulosScrape) TraverseContentTable(n *html.Node, depth int) {
	// Level 0 is TABLE
	// Level 1 is TBODY
	// Level 2 is TR
	if depth == 2 && n.Type == html.ElementNode && n.DataAtom == atom.Tr {
		row := s.TraverseContentRow(n, depth, nil)
		s.ProcessRowData(row)
	} else {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			s.TraverseContentTable(c, depth + 1)
		}
	}
}

func (s *trulosScrape) TraverseContentRow(n *html.Node, depth int, 
	data []string) []string {
	// Level 3 is TD
	// Level 4 is FONT
	// Level 5 and higher are target data
	if depth > 3 && n.Type == html.TextNode {
		data = append(data, n.Data)
	} else {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			data = s.TraverseContentRow(c, depth + 1, data)
		}
	}
	return data
}

func (s *trulosScrape) TraverseHTML(n *html.Node) {
	if n.Type == html.ElementNode && n.DataAtom == atom.Table &&
		attrIdIs(n, contentId) {
		s.TraverseContentTable(n, 0)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		s.TraverseHTML(c)
	}
}

func (s *trulosScrape) ProcessRowData(row []string) {
	var trimmed []string
	for _, item := range row {
		str := strings.TrimSpace(item)
		if len(str) == 0 {
			continue
		}
		trimmed = append(trimmed, str)
	}
	dateStr := trimmed[0]
	dateSplit := strings.Split(dateStr, "/")
	if len(dateSplit) != 3 {
		log.Println("Bad date:", dateStr, s, trimmed)
		return
	}
	dateYear, _ := strconv.Atoi(dateSplit[2])
	dateMonth, _ := strconv.Atoi(dateSplit[0])
	dateDay, _ := strconv.Atoi(dateSplit[1])
	date := time.Date(dateYear, time.Month(dateMonth), 
		dateDay, 12, 0, 0, 0, time.Local)
	origin := trimmed[1]
	if trimmed[2] != s.state.name {
		log.Println("Unexpected state:", trimmed[2], s, trimmed)
		return
	}
	llen, _ := strconv.Atoi(trimmed[6])
	if trimmed[7] != s.equip {
		log.Println("Unexpected equipment type:", trimmed[7], s, trimmed)
		return
	}
	price, _ := strconv.ParseFloat(trimmed[8], 64)
	weight, _ := strconv.Atoi(trimmed[9])
	stops, _ := strconv.Atoi(trimmed[10])
	if weight < 100 {
		weight *= 1000  // Assume * thousand pounds
	}
	load := &Load{date, origin, s.state.name, trimmed[3], trimmed[4], 
		trimmed[5], llen, weight, s.equip, price, stops, trimmed[12]}
		
	fmt.Println("Got a load", load)
}

func (t *trulosBoard) String() string {
	return fmt.Sprintf("%s -> %s", t.host, t.states)
}

func (s *trulosState) String() string {
	return fmt.Sprintf("%s [%s]", s.name, s.uri)
}

func (s *trulosScrape) String() string {
	return s.Id()
}
