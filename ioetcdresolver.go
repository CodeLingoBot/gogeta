package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"time"

	"github.com/golang/glog"
)

const (
	SERVICE_DOMAINTYTPE = "service"
	URI_DOMAINTYPE      = "uri"
	TIME_FORMAT         = "2006-01-02 15:04:05"
)

type Domain struct {
	typ   string
	value string
}

type location struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func (s *location) equals(other *location) bool {
	if s == nil && other == nil {
		return true
	}

	return s != nil && other != nil &&
		s.Host == other.Host &&
		s.Port == other.Port
}

type Service struct {
	index    string
	nodeKey  string
	location *location
	domain   string
	name     string
	status   *Status
	lastAccess *time.Time
}

type IoEtcdResolver struct {
	config          *Config
	watcher         *watcher
	domains         map[string]*Domain
	services        map[string]*ServiceCluster
	dest2ProxyCache map[string]http.Handler
	watchIndex      uint64
}

func NewEtcdResolver(c *Config) (*IoEtcdResolver, error) {
	domains := make(map[string]*Domain)
	services := make(map[string]*ServiceCluster)
	dest2ProxyCache := make(map[string]http.Handler)
	w, error := NewEtcdWatcher(c, domains, services)

	if error != nil {
		return nil, error
	}

	return &IoEtcdResolver{c, w, domains, services, dest2ProxyCache, 0}, nil
}

func (r *IoEtcdResolver) init() {
	r.watcher.init()
}

func (domain *Domain) equals(other *Domain) bool {
	if domain == nil && other == nil {
		return true
	}

	return domain != nil && other != nil &&
		domain.typ == other.typ && domain.value == other.value
}

func (service *Service) equals(other *Service) bool {
	if service == nil && other == nil {
		return true
	}

	return service != nil && other != nil &&
		service.location.equals(other.location) &&
		service.status.equals(other.status)
}

func (r *IoEtcdResolver) resolve(domainName string) (http.Handler, error) {
	glog.V(5).Infof("Looking for domain : %s ", domainName)
	domain := r.domains[domainName]
	glog.V(5).Infof("Services:%s",r.services)
	if domain != nil {
		service := r.services[domain.value]
		if service == nil {
			glog.Errorf("The services map doesn't contain service with the domain value: %s", domain.value)
		}
		switch domain.typ {

		case SERVICE_DOMAINTYTPE:
			if service, err := r.services[domain.value].Next(); err == nil {

				addr := net.JoinHostPort(service.location.Host, strconv.Itoa(service.location.Port))
				uri := fmt.Sprintf("http://%s/", addr)
				r.setLastAccessTime(service)
				return r.getOrCreateProxyFor(uri), nil

			} else {
				return nil, err
			}
		case URI_DOMAINTYPE:
			return r.getOrCreateProxyFor(domain.value), nil
		}

	}
	glog.V(5).Infof("Domain %s not found", domainName)
	return nil, errors.New("Domain not found")
}

func (r *IoEtcdResolver) setLastAccessTime(service *Service) {

	interval := time.Duration(r.config.lastAccessInterval) * time.Second
	if service.lastAccess == nil || service.lastAccess.Add(interval).Before(time.Now()) {
		lastAccessKey := fmt.Sprintf("%s/lastAccess", service.nodeKey)


		client, error := r.config.getEtcdClient()
		if error != nil {
			glog.Errorf("Unable to get etcd client : %s ", error)
			return
		}

		now := time.Now()
		service.lastAccess = &now

		t := service.lastAccess.Format(TIME_FORMAT)
		_, error = client.Set(lastAccessKey, t, 0)

		glog.V(5).Infof("Settign lastAccessKey to :%s", t)
		if error != nil {
			glog.Errorf("error :%s", error)
		}
	}

}

func (r *IoEtcdResolver) getOrCreateProxyFor(uri string) http.Handler {
	if _, ok := r.dest2ProxyCache[uri]; !ok {
		dest, _ := url.Parse(uri)
		r.dest2ProxyCache[uri] = httputil.NewSingleHostReverseProxy(dest)
	}
	return r.dest2ProxyCache[uri]
}
