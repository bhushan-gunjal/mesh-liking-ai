// Copyright (c) Facebook, Inc. and its affiliates. All Rights Reserved.

package fbnel

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/facebookresearch/Clinical-Trial-Parser/src/common/col/set"
	"github.com/facebookresearch/Clinical-Trial-Parser/src/common/conf"
	"github.com/facebookresearch/Clinical-Trial-Parser/src/common/util/fio"
	"github.com/facebookresearch/Clinical-Trial-Parser/src/common/util/slice"
	"github.com/facebookresearch/Clinical-Trial-Parser/src/common/util/timer"
	"github.com/facebookresearch/Clinical-Trial-Parser/src/vocabularies"
	"github.com/facebookresearch/Clinical-Trial-Parser/src/vocabularies/mesh"
	"github.com/facebookresearch/Clinical-Trial-Parser/src/vocabularies/taxonomy"
	"github.com/facebookresearch/Clinical-Trial-Parser/src/vocabularies/umls"

	"github.com/golang/glog"
)

var (
	reParentheses = regexp.MustCompile(`\([^)]*(\)|$)`)
	reConjunction = regexp.MustCompile(` and | or |,`)
)

// main matches (grounds) extracted (input) terms to vocabulary concepts.
// Matching results are written to a file. This version does not use clustering.

// Slot defines the extracted NER slot.
type Slot struct {
	label string  // Slot label
	term  string  // Slot term
	score float64 // NER score
}

type Slots []Slot

func NewSlot(label string, term string, score float64) Slot {
	return Slot{label: label, term: term, score: score}
}

func (s Slot) SubTerms() []string {
	v := reConjunction.Split(s.term, -1)
	slice.TrimSpace(v)
	return slice.RemoveEmpty(v)
}

func (s *Slot) Normalize(normalize taxonomy.Normalizer) {
	_, s.term = normalize(s.term)
}

func (s Slot) String() string {
	return fmt.Sprintf("%s\t%s\t%.3f", s.label, s.term, s.score)
}

func NewSlots() Slots {
	return make(Slots, 0)
}

func (ss *Slots) Add(label string, term string, score float64) {
	s := NewSlot(label, term, score)
	*ss = append(*ss, s)
}

func (ss Slots) Size() int {
	return len(ss)
}

// Matcher defines the struct that matches extracted terms to concepts
// from a vocabulary.
type Matcher struct {
	parameters conf.Config
	vocabulary *taxonomy.Taxonomy
	normalize  taxonomy.Normalizer
	clock      timer.Timer
}

// NewMatcher creates a new matcher.
func NewMatcher() *Matcher {
	clock := timer.New()
	return &Matcher{clock: clock}
}

func RunNel(input string) {
	m := NewMatcher()
	termStr := input
	if err := m.LoadParameters(); err != nil {
		glog.Fatal(err)
	}
	if err := m.LoadVocabulary(); err != nil {
		glog.Fatal(err)
	}
	// if err := m.Match(termStr); err != nil {
	// 	glog.Fatal(err)
	// }
	fmt.Println(m.Match(termStr))
	m.Close()
}

// LoadParameters loads parameters from command line and a config file.
func (m *Matcher) LoadParameters() error {
	file, err := conf.Load("nel.conf")
	if err != nil {
		fmt.Println(err)
	}
	m.parameters = file

	return nil
}

func (m *Matcher) LoadVocabulary() error {
	vocabularyFname := m.parameters.Get("vocabulary_file")
	var customFnames []string
	if m.parameters.Exists("custom_vocabulary_file") {
		path := m.parameters.Get("custom_vocabulary_file")
		customFnames = fio.ReadFnames(path)
	}

	source := vocabularies.ParseSource(m.parameters.Get("vocabulary_source"))
	var vocabulary *taxonomy.Taxonomy
	switch source {
	case vocabularies.MESH:
		vocabulary = mesh.Load(vocabularyFname, customFnames...)
	case vocabularies.UMLS:
		vocabulary = umls.Load(vocabularyFname)
	default:
		return fmt.Errorf("unknown vocabulary source")
	}

	rows := m.parameters.GetInt("lsh_rows")
	bands := m.parameters.GetInt("lsh_rows")

	m.normalize = mesh.Normalize
	vocabulary.Normalize(m.normalize)
	vocabulary.SetHashIndex(rows, bands)
	vocabulary.Info()

	m.vocabulary = vocabulary

	return nil
}

// getNERSlots gets the extracted terms from a string.
func getNERSlots(termStr string, nerThreshold float64, validLabels set.Set) Slots {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(termStr), &data); err != nil {
		glog.Fatal(termStr, err)
	}
	slots := NewSlots()
	for label, values := range data {
		if validLabels.Contains(label) {
			for _, fields := range values.([]interface{}) {
				var term string
				var score float64
				for _, f := range fields.([]interface{}) {
					switch f.(type) {
					case string:
						term = f.(string)
					case float64:
						score = f.(float64)
					default:
						glog.Fatalf("unknown type: %v", f)
					}
				}
				norm := reParentheses.ReplaceAllString(term, " ")
				norm = strings.TrimSpace(norm)
				if len(norm) > 0 {
					term = norm
				}
				if score > nerThreshold && len(term) > 0 {
					slots.Add(label, term, score)
				}
			}
		}
	}
	return slots
}

type ResultDic struct {
	Category string 
	Concepts string
	Tree string
	Score float64
}

func (m *Matcher) Match(termStr string) ResultDic{
	nerThreshold := m.parameters.GetFloat64("ner_threshold")
	validLabels := set.New(m.parameters.GetSlice("valid_labels", ",")...)

	matchThreshold := m.parameters.GetFloat64("match_threshold")
	matchMargin := m.parameters.GetFloat64("match_margin")

	matchedSlots := make(map[string]taxonomy.Terms)
	conceptSet := set.New()
	slotCnt := 0
	
	defaultCategories := set.New()
	cancerCategories := set.New("C")
	personCategories := set.New("M")

	//termStr :=
	slots := getNERSlots(termStr, nerThreshold, validLabels)
	slotCnt += slots.Size()

	// Match NER terms to concepts
	for _, slot := range slots {
		subterms := slot.SubTerms()
		for _, subterm := range subterms {
			if _, ok := matchedSlots[subterm]; !ok {
				var validCategories set.Set
				switch slot.label {
				case "word_scores:cancer":
					validCategories = cancerCategories
				case "word_scores:gender":
					validCategories = personCategories
				default:
					validCategories = defaultCategories
				}
				matchedSlots[subterm] = m.vocabulary.Match(subterm, matchMargin, validCategories)
			}
		}

		slot.Normalize(m.normalize)
		

		for _, subterm := range subterms {
			matchedConcepts := matchedSlots[subterm]
			if matchedConcepts.MaxValue() >= matchThreshold {
				conceptSet.Add(matchedConcepts.Keys()...)
				concepts := strings.Join(matchedConcepts.Keys(), "|")
				nelScore := matchedConcepts.MaxValue()
				treeNumbers := strings.Join(matchedConcepts.TreeNumbers(), "|")
				d := ResultDic{slot.String(), concepts, treeNumbers,nelScore}
				return d
			}
		}

	}
	return ResultDic{}
}

// Close closes the matcher.
func (m *Matcher) Close() {
	glog.Flush()
}
