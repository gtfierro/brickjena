package main

import (
	"crypto/md5"
	"encoding/binary"
	"flag"
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"
)

var fuseki = flag.String("a", "http://localhost:80", "Fuseki address")
var db = flag.String("db", "SodaHall", "Fuseki db to run on")
var query = flag.String("q", "query.rq", "sparql query file")
var docount = flag.Bool("c", true, "if true, only print out count")
var port = flag.String("p", "1212", "Port to serve")
var fc *FusekiConn

var savedQueries = []Q{
	{
		"Sensors in AHU",
		html.EscapeString("SELECT ?ahu ?sensor\nWHERE {\n    ?ahu rdf:type/rdfs:subClassOf* brick:AHU .\n    ?ahu (bf:feeds|bf:hasPoint|bf:hasPart|bf:contains)* ?sensor .\n\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Reheat_Valve_Command }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Cooling_Valve_Command }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Mixed_Air_Temperature_Sensor }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Outside_Air_Temperature_Sensor }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Return_Air_Temperature_Sensor }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Supply_Air_Temperature_Sensor }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Outside_Air_Humidity_Sensor }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Return_Air_Temperature_Sensor}\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Outside_Air_Damper_Position_Sensor }\n}\n"),
	},
	{
		"Zone Temperature Sensors",
		html.EscapeString("SELECT DISTINCT ?sensor ?room\nWHERE {\n    ?vav rdf:type brick:VAV .\n    ?zone rdf:type brick:HVAC_Zone .\n    ?room rdf:type brick:Room .\n\n    ?sensor rdf:type/rdfs:subClassOf* brick:Zone_Temperature_Sensor . \n\n    ?vav bf:feeds+ ?zone .\n    ?zone bf:hasPart ?room .\n\n    {?sensor bf:isPointOf ?vav }\n    UNION\n    {?sensor bf:isPointOf ?room }\n}\n"),
	},
	{
		"CO2, Occupancy Sensors",
		html.EscapeString("SELECT DISTINCT ?sensor ?vav\nWHERE {\n\n      { ?sensor rdf:type/rdfs:subClassOf* brick:Occupancy_Sensor . }\n        UNION\n      { ?sensor rdf:type/rdfs:subClassOf* brick:CO2_Sensor . }\n\n    ?room rdf:type brick:Room .\n    ?sensor bf:isPointOf ?room .\n\n}"),
	},
	{
		"Power Meters",
		html.EscapeString("SELECT ?meter ?loc\nWHERE {\n    ?meter rdf:type/rdfs:subClassOf* brick:Power_Meter .\n    ?loc rdf:type ?loc_class .\n    ?loc_class rdfs:subClassOf+ brick:Location .\n\n    ?loc bf:hasPoint ?meter .\n}\n"),
	},
	{
		"Equipment In Rooms",
		html.EscapeString("SELECT ?equipment ?room\nWHERE {\n    ?room rdf:type brick:Room .\n\n    ?equipment bf:isLocatedIn ?room .\n\n    { ?equipment rdf:type/rdfs:subClassOf* brick:Lighting_System .}\n    UNION\n    { ?equipment rdf:type/rdfs:subClassOf* brick:Heating_Ventilation_Air_Conditioning_System .}\n}\n}"),
	},
	{
		"Reheat/Cool Valve cmd for VAV",
		html.EscapeString("SELECT ?vlv_cmd ?vav\nWHERE {\n    {\n      { ?vlv_cmd rdf:type brick:Reheat_Valve_Command }\n      UNION\n      { ?vlv_cmd rdf:type brick:Cooling_Valve_Command }\n    }\n    ?vav rdf:type brick:VAV .\n    ?vav bf:hasPoint+ ?vlv_cmd .\n}\n"),
	},
}

type FusekiConn struct {
	addr   string
	client *http.Client
}

func NewFusekiConn(addr string) *FusekiConn {
	fc := &FusekiConn{
		addr:   strings.TrimSuffix(addr, "/"),
		client: &http.Client{},
	}
	return fc
}

func (fc *FusekiConn) Query(dataset, query string) ([][]URI, error) {
	req, err := http.NewRequest("GET", fc.addr+"/"+dataset, nil)
	if err != nil {
		return [][]URI{}, err
	}

	query = "PREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> \nPREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#> \nPREFIX bf: <http://buildsys.org/ontologies/BrickFrame#>\nPREFIX brick: <http://buildsys.org/ontologies/Brick#> \nPREFIX btag: <http://buildsys.org/ontologies/BrickTag#> \nPREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> \nPREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#> \nPREFIX soda_hall: <http://buildsys.org/ontologies/building_example#> \n" + query

	q := req.URL.Query()
	q.Add("query", query)
	req.URL.RawQuery = q.Encode()
	req.Header.Add("Accept", "application/sparql-results+json")

	resp, err := fc.client.Do(req)
	if err != nil {
		return [][]URI{}, err
	}
	return ParseResponse(resp.Body)
}

//func main() {
//	flag.Parse()
//	fc := NewFusekiConn(*fuseki)
//	f, err := os.Open(*query)
//	if err != nil {
//		log.Fatal(err)
//	}
//	content, err := ioutil.ReadAll(f)
//	if err != nil {
//		log.Fatal(err)
//	}
//	results, err := fc.Query(*db, string(content))
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	if *docount {
//		fmt.Println(len(results))
//	} else {
//		for _, r := range results {
//			fmt.Println(r)
//		}
//	}
//}

type Q struct {
	Name string
	Body string
}

type TemplateContext struct {
	Token   string
	Queries []Q
	Results [][]URI
}

func index(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	template, err := template.ParseFiles("index.template")
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
		return
	}
	// get a token
	h := md5.New()
	seed := make([]byte, 16)
	binary.PutVarint(seed, time.Now().UnixNano())
	h.Write(seed)
	token := fmt.Sprintf("%x", h.Sum(nil))
	tc := TemplateContext{
		Token:   token,
		Queries: savedQueries,
	}
	template.Execute(w, tc)
}

func handlequery(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	query := req.FormValue("query")
	log.Println(query)
	building := req.FormValue("building")
	log.Println(building)
	if len(query) == 0 {
		index(w, req)
		return
	}
	results, err := fc.Query(building, query)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
		return
	}
	template, err := template.ParseFiles("index.template")
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
		return
	}
	// get a token
	h := md5.New()
	seed := make([]byte, 16)
	binary.PutVarint(seed, time.Now().UnixNano())
	h.Write(seed)
	token := fmt.Sprintf("%x", h.Sum(nil))
	tc := TemplateContext{
		Token:   token,
		Queries: savedQueries,
		Results: results,
	}
	template.Execute(w, tc)
}

func main() {
	flag.Parse()
	fc = NewFusekiConn(*fuseki)
	http.HandleFunc("/", index)
	http.HandleFunc("/query", handlequery)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	log.Printf("Serving on %s...\n", ":"+*port)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
