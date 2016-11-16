package main

import (
	"crypto/md5"
	"encoding/binary"
	"flag"
	"fmt"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"html"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

var fuseki = flag.String("a", "http://localhost:80", "Fuseki address")
var db = flag.String("db", "SodaHall", "Fuseki db to run on")
var query = flag.String("q", "query.rq", "sparql query file")
var docount = flag.Bool("c", true, "if true, only print out count")
var port = flag.String("p", "1212", "Port to serve")
var serve = flag.Bool("s", true, "Port to serve")
var fc *FusekiConn

var savedQueries = []Q{
	{
		"Sensors in AHU",
		html.EscapeString("SELECT ?ahu ?sensor\nWHERE {\n    ?ahu rdf:type/rdfs:subClassOf* brick:AHU .\n    ?ahu (bf:feeds|bf:hasPoint|bf:hasPart|bf:contains)* ?sensor .\n\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Reheat_Valve_Command }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Cooling_Valve_Command }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Mixed_Air_Temperature_Sensor }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Outside_Air_Temperature_Sensor }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Return_Air_Temperature_Sensor }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Supply_Air_Temperature_Sensor }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Outside_Air_Humidity_Sensor }\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Return_Air_Temperature_Sensor}\n    UNION\n    { ?sensor rdf:type/rdfs:subClassOf* brick:Outside_Air_Damper_Position_Sensor }\n}\n"),
	},
	{
		"Zone Temperature Sensors",
		html.EscapeString("SELECT DISTINCT ?sensor ?room\nWHERE {\n\n    ?sensor rdf:type/rdfs:subClassOf* brick:Zone_Temperature_Sensor .\n    ?room rdf:type brick:Room .\n    ?vav rdf:type brick:VAV .\n    ?zone rdf:type brick:HVAC_Zone .\n\n    ?vav bf:feeds+ ?zone .\n    ?zone bf:hasPart ?room .\n\n    {?sensor bf:isPointOf ?vav }\n    UNION\n    {?sensor bf:isPointOf ?room }\n}"),
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
		html.EscapeString("SELECT ?equipment ?room\nWHERE {\n    ?room rdf:type brick:Room .\n\n    ?equipment bf:isLocatedIn ?room .\n\n    { ?equipment rdf:type/rdfs:subClassOf* brick:Lighting_System .}\n    UNION\n    { ?equipment rdf:type/rdfs:subClassOf* brick:Heating_Ventilation_Air_Conditioning_System .}\n}"),
	},
	{
		"Reheat/Cool Valve cmd for VAV",
		html.EscapeString("SELECT ?vlv_cmd ?vav\nWHERE {\n    {\n      { ?vlv_cmd rdf:type brick:Reheat_Valve_Command }\n      UNION\n      { ?vlv_cmd rdf:type brick:Cooling_Valve_Command }\n    }\n    ?vav rdf:type brick:VAV .\n    ?vav bf:hasPoint+ ?vlv_cmd .\n}\n"),
	},
	{
		"Map VAVs to Zones and Rooms",
		html.EscapeString("SELECT ?vav ?room\nWHERE {\n    ?vav rdf:type brick:VAV .\n    ?room rdf:type brick:Room .\n    ?zone rdf:type brick:HVAC_Zone .\n    ?vav bf:feeds+ ?zone .\n    ?room bf:isPartOf ?zone .\n}\n"),
	},
	{
		"Map out floors, HVAC zones and rooms",
		html.EscapeString("SELECT ?floor ?room ?zone\nWHERE {\n    ?floor rdf:type brick:Floor .\n    ?room rdf:type brick:Room .\n    ?zone rdf:type brick:HVAC_Zone .\n\n    ?room bf:isPartOf+ ?floor .\n    ?room bf:isPartOf+ ?zone .\n}"),
	},
	{
		"Associate lighting with rooms",
		html.EscapeString("SELECT DISTINCT ?light_equip ?light_state ?light_cmd ?room\nWHERE {\n\n    ?light_equip rdf:type/rdfs:subClassOf* brick:Lighting_System .\n\n    ?light_equip bf:feeds ?zone .\n    ?zone rdf:type brick:Lighting_Zone .\n    ?zone bf:contains ?room .\n    ?room rdf:type brick:Room .\n\n    ?light_state rdf:type/rdfs:subClassOf* brick:Luminance_Status .\n    ?light_cmd rdf:type/rdfs:subClassOf* brick:Luminance_Command .\n\n    {?light_equip bf:hasPoint ?light_state}\n    UNION\n    {?zone bf:hasPoint ?light_state}\n\n    {?light_equip bf:hasPoint ?light_cmd}\n    UNION\n    {?zone bf:hasPoint ?light_cmd}\n}"),
	},
	{
		"Equipment and their power meters",
		html.EscapeString("SELECT ?x ?meter\nWHERE {\n    ?meter rdf:type/rdfs:subClassOf* brick:Power_Meter .\n    ?meter (bf:isPointOf|bf:isPartOf)* ?x .\n    {?x rdf:type/rdfs:subClassOf* brick:Equipment .}\n    UNION\n    {?x rdf:type/rdfs:subClassOf* brick:Location .}\n}\n"),
	},
	{
		"Equipment with power meters and control points",
		html.EscapeString("SELECT DISTINCT ?equip ?cmd ?status\nWHERE {\n    ?equip  rdf:type/rdfs:subClassOf* brick:Equipment .\n    {\n    ?cmd rdf:type/rdfs:subClassOf*    brick:Command .\n    ?cmd (bf:isPointOf|bf:isPartOf)* ?equip .\n    }\n    UNION\n    {\n    ?status rdf:type/rdfs:subClassOf* brick:Status .\n    ?status (bf:isPointOf|bf:isPartOf)* ?equip .\n    }\n}"),
	},
}

type FusekiConn struct {
	addr    string
	client  *http.Client
	queries *mgo.Collection
}

func NewFusekiConn(addr string) *FusekiConn {
	session, err := mgo.Dial("0.0.0.0:27017")
	if err != nil {
		log.Fatal(err)
	}
	brickdb := session.DB("brickjena")
	fc := &FusekiConn{
		addr:    strings.TrimSuffix(addr, "/"),
		client:  &http.Client{},
		queries: brickdb.C("queries"),
	}
	//	index := mgo.Index{
	//		Key:        []string{"query", "building"},
	//		Unique:     true,
	//		DropDups:   true,:
	//		Background: false,
	//		Sparse:     false,
	//	}
	//	err = fc.queries.EnsureIndex(index)
	//	if err != nil {
	//		log.Fatalf("Could not create index (%v)", err)
	//	}
	return fc
}

func (fc *FusekiConn) Query(dataset, query string) ([][]URI, error) {
	req, err := http.NewRequest("GET", fc.addr+"/"+dataset, nil)
	if err != nil {
		return [][]URI{}, err
	}

	query = "PREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> \nPREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#> \nPREFIX bf: <http://buildsys.org/ontologies/BrickFrame#>\nPREFIX brick: <http://buildsys.org/ontologies/Brick#> \nPREFIX btag: <http://buildsys.org/ontologies/BrickTag#> \nPREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> \nPREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#> \nPREFIX soda_hall: <http://buildsys.org/ontologies/building_example#> \n" + query

	var results bson.M
	// check for query
	err = fc.queries.Find(bson.M{"query": query, "building": dataset}).Select(bson.M{"results": 1}).One(&results)
	if err != nil && err != mgo.ErrNotFound {
		return [][]URI{}, err
	} else if err == nil {
		var ret = [][]URI{}
		newlist := results["results"].([]interface{})
		for _, trip := range newlist {
			triple := []URI{}
			tt := trip.([]interface{})
			for _, ttt := range tt {
				s := ttt.(bson.M)
				triple = append(triple, URI{s["namespace"].(string), s["value"].(string)})
			}
			ret = append(ret, triple)
		}
		fmt.Println(len(ret))
		return ret, nil
	}

	q := req.URL.Query()
	q.Add("query", query)
	req.URL.RawQuery = q.Encode()
	req.Header.Add("Accept", "application/sparql-results+json")

	resp, err := fc.client.Do(req)
	if err != nil {
		return [][]URI{}, err
	}
	uris, err := ParseResponse(resp.Body)
	if err != nil {
		return [][]URI{}, err
	}
	// insert!
	newdoc := bson.M{
		"query":    query,
		"building": dataset,
		"results":  uris,
	}
	err = fc.queries.Insert(newdoc)

	return uris, err
}

//func main() {
//	flag.Parse()
//	fc := NewFusekiConn(*fuseki)
//}

type Q struct {
	Name string
	Body string
}

type TemplateContext struct {
	Token    string
	Selected map[string]string
	Queries  []Q
	Results  [][]URI
	Chosen   Q
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
		Selected: map[string]string{
			"SodaHall":        "notselected",
			"RiceHall":        "notselected",
			"EBU3B":           "notselected",
			"Gates":           "notselected",
			"GreenTechCenter": "notselected",
		},
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
		Selected: map[string]string{
			"SodaHall":        "notselected",
			"RiceHall":        "notselected",
			"EBU3B":           "notselected",
			"Gates":           "notselected",
			"GreenTechCenter": "notselected",
		},
		Chosen: Q{Body: query},
	}
	tc.Selected[building] = "selected"
	log.Println(tc.Selected)
	template.Execute(w, tc)
}

func main() {
	flag.Parse()
	log.Println(*fuseki)
	fc = NewFusekiConn(*fuseki)
	if *serve {
		http.HandleFunc("/", index)
		http.HandleFunc("/query", handlequery)
		http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
		log.Printf("Serving on %s...\n", ":"+*port)
		log.Fatal(http.ListenAndServe(":"+*port, nil))
	}
	f, err := os.Open(*query)
	if err != nil {
		log.Fatal(err)
	}
	content, err := ioutil.ReadAll(f)
	if err != nil {
		log.Fatal(err)
	}
	results, err := fc.Query(*db, string(content))
	if err != nil {
		log.Fatal(err)
	}

	if *docount {
		fmt.Println(len(results))
	} else {
		for _, r := range results {
			fmt.Println(r)
		}
	}
}
