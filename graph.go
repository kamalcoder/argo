package argo

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
)

// A Graph wraps a Store and provides extra convenience methods.
type Graph struct {
	// The associated triple store.
	Store Store

	// Mutex locking the store.
	Mutex sync.Mutex

	// The prefix map.
	Prefixes map[string]string
}

// Function NewGraph creates and returns a new graph.
func NewGraph(store Store) (graph *Graph) {
	return &Graph{
		Store:    store,
		Prefixes: map[string]string{"http://www.w3.org/1999/02/22-rdf-syntax-ns#": "rdf"},
	}
}

// Function Bind adds the given URI/prefix mapping to the internal map, and returns the uri wrapped
// in a Namespace for your convenience.
func (graph *Graph) Bind(uri string, prefix string) (ns Namespace) {
	graph.Prefixes[uri] = prefix
	return NewNamespace(uri)
}

// Function LookupAndBind looks up the prefix using the prefix.cc service, then maps the prefix to
// the returned URI and returns the URI wrapped in a Namespace for your convenience.
func (graph *Graph) LookupAndBind(prefix string) (ns Namespace, err error) {
	uri, err := LookupPrefix(prefix)
	if err != nil {
		return ns, err
	}

	return graph.Bind(uri, prefix), nil
}

// Function Add adds the given triple to the graph and returns its index.
func (graph *Graph) Add(triple *Triple) (index int) {
	graph.Mutex.Lock()
	defer graph.Mutex.Unlock()

	return graph.Store.Add(triple)
}

// Function AddTriple creates a triple from the arguments and adds it to the graph.
func (graph *Graph) AddTriple(subject Term, predicate Term, object Term) (index int) {
	return graph.Add(NewTriple(subject, predicate, object))
}

// Function AddQuad creates a quad from the arguments and adds it to the graph.
func (graph *Graph) AddQuad(subject Term, predicate Term, object Term, context Term) (index int) {
	return graph.Add(NewQuad(subject, predicate, object, context))
}

// Function EncodeContainer returns a channel. Every value sent on the channel will be added to a
// container with the given subject. The channel should be closed when finished with. A type is not
// added to the subject automatically.
func (graph *Graph) EncodeContainer(subject Term) (ch chan Term) {
	ch = make(chan Term)

	go func() {
		i := 1

		for term := range ch {
			graph.AddTriple(subject, RDF.Get(fmt.Sprintf("_%d", i)), term)
			i++
		}
	}()

	return ch
}

// Function EncodeList returns a channel. Every value sent on the channel will be added to a list
// with the given subject. The channel should be closed when finished with.
func (graph *Graph) EncodeList(subject Term) (ch chan Term) {
	ch = make(chan Term)

	go func() {
		term, ok := <-ch
		if ok {
			graph.AddTriple(subject, A, RDF.Get("List"))
			graph.AddTriple(subject, RDF.Get("first"), term)

			for term = range ch {
				next := NewAnonNode()
				graph.AddTriple(subject, RDF.Get("rest"), next)
				subject = next

				graph.AddTriple(subject, A, RDF.Get("List"))
				graph.AddTriple(subject, RDF.Get("first"), term)
			}

			graph.AddTriple(subject, RDF.Get("rest"), RDF.Get("nil"))
		}
	}()

	return ch
}

// Function Remove removes the given triple from the graph, if it exists.
func (graph *Graph) Remove(triple *Triple) {
	graph.Mutex.Lock()
	defer graph.Mutex.Unlock()

	graph.Store.Remove(triple)
}

// Function RemoveIndex removes the triple with the given index from the graph, if it exists.
func (graph *Graph) RemoveIndex(index int) {
	graph.Mutex.Lock()
	defer graph.Mutex.Unlock()

	graph.Store.RemoveIndex(index)
}

// Function Remove removes the given triple from the graph, if it exists.
func (graph *Graph) RemoveTriple(subject Term, predicate Term, object Term) {
	graph.Remove(NewTriple(subject, predicate, object))
}

// Function Remove removes the given quad from the graph, if it exists.
func (graph *Graph) RemoveQuad(subject Term, predicate Term, object Term, context Term) {
	graph.Remove(NewQuad(subject, predicate, object, context))
}

// Function Clear clears the graph.
func (graph *Graph) Clear() {
	graph.Mutex.Lock()
	defer graph.Mutex.Unlock()

	graph.Store.Clear()
}

// Function Num returns the number of triples in the graph.
func (graph *Graph) Num() (n int) {
	graph.Mutex.Lock()
	defer graph.Mutex.Unlock()

	return graph.Store.Num()
}

// Function IterTriples returns a channel that will yield the triples of the graph. The channel will
// be closed when iteration is completed.
func (graph *Graph) IterTriples() (ch chan *Triple) {
	graph.Mutex.Lock()
	defer graph.Mutex.Unlock()

	return graph.Store.IterTriples()
}

// Function Filter returns a channel that will yield all matching triples of the graph. A nil value
// passed means that the check for this term is skipped; else the triples returned must have the
// same terms as the corresponding arguments.
func (graph *Graph) Filter(subjSearch, predSearch, objSearch Term) (ch chan *Triple) {
	graph.Mutex.Lock()
	defer graph.Mutex.Unlock()

	return graph.Store.Filter(subjSearch, predSearch, objSearch)
}

// Function FilterSubset adds the triples returned by Filter(subjSearch, predSearch, objSearch) to
// the specified graph.
func (graph *Graph) FilterSubset(subGraph *Graph, subjSearch, predSearch, objSearch Term) {
	for triple := range graph.Filter(subjSearch, predSearch, objSearch) {
		subGraph.Add(triple)
	}
}

// Function HasSubject returns where the specified term is present as a subject in the graph.
func (graph *Graph) HasSubject(subject Term) (result bool) {
	ch := graph.Filter(subject, nil, nil)
	_, result = <-ch

	for _ = range ch {
	}

	return result
}

// Function GetAll returns all objects with the given subject and predicate.
func (graph *Graph) GetAll(subject Term, predicate Term) (objects []Term) {
	objects = make([]Term, 0)

	for triple := range graph.IterTriples() {
		if triple.Subject == subject && triple.Predicate == predicate {
			objects = append(objects, triple.Object)
		}
	}

	return objects
}

// Function Get returns the first object with the given subject and predicate, or nil if it was not
// found.
func (graph *Graph) Get(subject Term, predicate Term) (object Term) {
	for triple := range graph.IterTriples() {
		if triple.Subject == subject && triple.Predicate == predicate {
			return triple.Object
		}
	}

	return nil
}

// Function MustGet returns the first object with the given subject and predicate, or panics if it
// was not found.
func (graph *Graph) MustGet(subject Term, predicate Term) (object Term) {
	object = graph.Get(subject, predicate)
	if object == nil {
		panic("Object not found in graph")
	}

	return object
}

// Function IterContainer returns a channel that yields successive items of an RDF container (Seq,
// Bag or Alt).
func (graph *Graph) IterContainer(root Term) (ch chan Term) {
	ch = make(chan Term)

	go func() {
		i := 0

		for {
			item := graph.Get(root, RDF.Get(fmt.Sprintf("_%d", i)))
			if item == nil {
				close(ch)
				return
			}

			ch <- item
			i++
		}
	}()

	return ch
}

// Function IterList returns a channel that yields successive items of an RDF List.
func (graph *Graph) IterList(root Term) (ch chan Term) {
	ch = make(chan Term)

	go func() {
		for {
			ch <- graph.Get(root, First)
			root = graph.Get(root, Rest)

			if root == Nil {
				close(ch)
				return
			}
		}
	}()

	return ch
}

// Function Parse uses the specified Parser to parse RDF from an io.Reader.
func (graph *Graph) Parse(parser Parser, r io.Reader) (err error) {
	tripleChan := make(chan *Triple)
	errChan := make(chan error)

	go parser(r, tripleChan, errChan)

	for triple := range tripleChan {
		graph.Add(triple)
	}

	return <-errChan
}

// Function ParseFile uses the specified Parser to parse RDF from a file.
func (graph *Graph) ParseFile(parser Parser, filename string) (err error) {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}

	defer f.Close()
	return graph.Parse(parser, f)
}

// Function ParseHTTP uses the specified Parser to parse RDF from the web. acceptMIMEType is the
// MIME type sent as the Accept header - it is used by some servers to determine which format the
// data should be returned in. If the empty string is passed, no header is sent. Common values are:
// 
// * RDF/XML: application/rdf+xml
// * NTriples: text/plain
// * Turtle: text/turtle
// * Notation3: text/n3
//
func (graph *Graph) ParseHTTP(parser Parser, url string, acceptMIMEType string) (err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	if acceptMIMEType != "" {
		req.Header.Add("Accept", acceptMIMEType)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP request returned status %d", resp.StatusCode)
	}

	return graph.Parse(parser, resp.Body)
}

// Function Serialize uses the specified Serializer to serialize an RDF file to an io.Writer.
func (graph *Graph) Serialize(serializer Serializer, w io.Writer) (err error) {
	errChan := make(chan error)

	serializer(w, graph.IterTriples(), errChan)

	return <-errChan
}
