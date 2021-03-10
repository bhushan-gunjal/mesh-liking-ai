package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"hekma_nl/model"

)

type json_input struct {
	NerEntity string `json:"nerentity"`
}

var m *fbnel.Matcher = fbnel.NewMatcher();


func Cleaner(w http.ResponseWriter, r *http.Request) {
	// Read body
	b, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
			
	// Unmarshal
	var input json_input
	err = json.Unmarshal(b, &input)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	type EntityResult struct {
		MeshEntity fbnel.ResultDic `json:"meshentity"`
	  }
	
	var ent fbnel.ResultDic = m.Match(input.NerEntity)
	
	EntNel := EntityResult{MeshEntity: ent,}
	
	jsonData, err := json.Marshal(EntNel)
	
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("content-type", "application/json")
	w.Write(jsonData)
	}

func main() { 
	m.LoadParameters();
	m.LoadVocabulary();
	http.HandleFunc("/", Cleaner)
	address := ":8000"
	log.Println("Starting server on address", address)
	err := http.ListenAndServe(address, nil)
	if err != nil {
		panic(err)
	}
}
