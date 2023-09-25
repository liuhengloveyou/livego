package core

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/liuhengloveyou/livego/common"
	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/externalcmd"
)

var DefaultPathManager *PathManager

// func PathConfCanBeUpdated(oldPathConf *conf.PathConf, newPathConf *conf.PathConf) bool {
// 	clone := oldPathConf.Clone()

// 	clone.RPICameraBrightness = newPathConf.RPICameraBrightness
// 	clone.RPICameraContrast = newPathConf.RPICameraContrast
// 	clone.RPICameraSaturation = newPathConf.RPICameraSaturation
// 	clone.RPICameraSharpness = newPathConf.RPICameraSharpness
// 	clone.RPICameraExposure = newPathConf.RPICameraExposure
// 	clone.RPICameraAWB = newPathConf.RPICameraAWB
// 	clone.RPICameraDenoise = newPathConf.RPICameraDenoise
// 	clone.RPICameraShutter = newPathConf.RPICameraShutter
// 	clone.RPICameraMetering = newPathConf.RPICameraMetering
// 	clone.RPICameraGain = newPathConf.RPICameraGain
// 	clone.RPICameraEV = newPathConf.RPICameraEV
// 	clone.RPICameraFPS = newPathConf.RPICameraFPS

// 	return newPathConf.Equal(clone)
// }

func getConfForPath(PathConfs map[string]*conf.PathConf, name string) (string, *conf.PathConf, []string, error) {
	err := conf.IsValidPathName(name)
	if err != nil {
		return "", nil, nil, fmt.Errorf("invalid Path name: %s (%s)", err, name)
	}

	// normal Path
	if PathConf, ok := PathConfs[name]; ok {
		return name, PathConf, nil, nil
	}

	// regular expression-based Path
	for PathConfName, PathConf := range PathConfs {
		if PathConf.Regexp != nil {
			m := PathConf.Regexp.FindStringSubmatch(name)
			if m != nil {
				return PathConfName, PathConf, m, nil
			}
		}
	}

	return "", nil, nil, fmt.Errorf("Path '%s' is not configured", name)
}

type PathManager struct {
	externalAuthenticationURL string
	rtspAddress               string
	authMethods               conf.AuthMethods
	readTimeout               conf.StringDuration
	writeTimeout              conf.StringDuration
	writeQueueSize            int
	udpMaxPayloadSize         int
	PathConfs                 map[string]*conf.PathConf
	externalCmdPool           *externalcmd.Pool
	metrics                   *Metrics

	ctx         context.Context
	ctxCancel   func()
	wg          sync.WaitGroup
	Paths       map[string]*Path
	PathsByConf map[string]map[*Path]struct{}

	// in
	// chReloadConf     chan map[string]*conf.PathConf
	chClosePath      chan *Path
	chPathReady      chan *Path
	chPathNotReady   chan *Path
	chGetConfForPath chan PathGetConfForPathReq
	chDescribe       chan PathDescribeReq
	chAddReader      chan PathAddReaderReq
	chAddPublisher   chan PathAddPublisherReq
	chAPIPathsList   chan PathAPIPathsListReq
	chAPIPathsGet    chan PathAPIPathsGetReq
}

func InitPathManager(
	externalAuthenticationURL string,
	rtspAddress string,
	authMethods conf.AuthMethods,
	readTimeout conf.StringDuration,
	writeTimeout conf.StringDuration,
	writeQueueSize int,
	udpMaxPayloadSize int,
	PathConfs map[string]*conf.PathConf,
	externalCmdPool *externalcmd.Pool,
	metrics *Metrics,
) {
	ctx, ctxCancel := context.WithCancel(context.Background())

	DefaultPathManager = &PathManager{
		externalAuthenticationURL: externalAuthenticationURL,
		rtspAddress:               rtspAddress,
		authMethods:               authMethods,
		readTimeout:               readTimeout,
		writeTimeout:              writeTimeout,
		writeQueueSize:            writeQueueSize,
		udpMaxPayloadSize:         udpMaxPayloadSize,
		PathConfs:                 PathConfs,
		externalCmdPool:           externalCmdPool,
		metrics:                   metrics,
		ctx:                       ctx,
		ctxCancel:                 ctxCancel,
		Paths:                     make(map[string]*Path),
		PathsByConf:               make(map[string]map[*Path]struct{}),
		// chReloadConf:              make(chan map[string]*conf.PathConf),
		chClosePath:      make(chan *Path),
		chPathReady:      make(chan *Path),
		chPathNotReady:   make(chan *Path),
		chGetConfForPath: make(chan PathGetConfForPathReq),
		chDescribe:       make(chan PathDescribeReq),
		chAddReader:      make(chan PathAddReaderReq),
		chAddPublisher:   make(chan PathAddPublisherReq),
		chAPIPathsList:   make(chan PathAPIPathsListReq),
		chAPIPathsGet:    make(chan PathAPIPathsGetReq),
	}

	for PathConfName, PathConf := range DefaultPathManager.PathConfs {
		if PathConf.Regexp == nil {
			DefaultPathManager.createPath(PathConfName, PathConf, PathConfName, nil)
		}
	}

	// if DefaultPathManager.metrics != nil {
	// 	DefaultPathManager.metrics.pathManagerSet(DefaultPathManager)
	// }

	DefaultPathManager.wg.Add(1)
	go DefaultPathManager.run()
}

func (pm *PathManager) close() {
	pm.ctxCancel()
	pm.wg.Wait()
}

func (pm *PathManager) run() {
	defer pm.wg.Done()

outer:
	for {
		select {
		// case newPathConfs := <-pm.chReloadConf:
		// 	pm.doReloadConf(newPathConfs)

		case pa := <-pm.chClosePath:
			pm.doClosePath(pa)

		case pa := <-pm.chPathReady:
			pm.doPathReady(pa)

		case pa := <-pm.chPathNotReady:
			pm.doPathNotReady(pa)

		case req := <-pm.chGetConfForPath:
			pm.doGetConfForPath(req)

		case req := <-pm.chDescribe:
			pm.doDescribe(req)

		case req := <-pm.chAddReader:
			common.Logger.Info("PathManager.run: ", "req", req)
			pm.doAddReader(req)

		case req := <-pm.chAddPublisher:
			pm.doAddPublisher(req)

		case req := <-pm.chAPIPathsList:
			pm.doAPIPathsList(req)

		case req := <-pm.chAPIPathsGet:
			pm.doAPIPathsGet(req)

		case <-pm.ctx.Done():
			break outer
		}
	}

	pm.ctxCancel()

	// if pm.metrics != nil {
	// 	pm.metrics.pathManagerSet(nil)
	// }
}

// func (pm *PathManager) doReloadConf(newPathConfs map[string]*conf.PathConf) {
// 	for confName, PathConf := range pm.PathConfs {
// 		if newPathConf, ok := newPathConfs[confName]; ok {
// 			// configuration has changed
// 			if !newPathConf.Equal(PathConf) {
// 				if PathConfCanBeUpdated(PathConf, newPathConf) { // Paths associated with the configuration can be updated
// 					for pa := range pm.PathsByConf[confName] {
// 						go pa.reloadConf(newPathConf)
// 					}
// 				} else { // Paths associated with the configuration must be recreated
// 					for pa := range pm.PathsByConf[confName] {
// 						pm.removePath(pa)
// 						pa.close()
// 						pa.wait() // avoid conflicts between sources
// 					}
// 				}
// 			}
// 		} else {
// 			// configuration has been deleted, remove associated Paths
// 			for pa := range pm.PathsByConf[confName] {
// 				pm.removePath(pa)
// 				pa.close()
// 				pa.wait() // avoid conflicts between sources
// 			}
// 		}
// 	}

// 	pm.PathConfs = newPathConfs

// 	// add new Paths
// 	for PathConfName, PathConf := range pm.PathConfs {
// 		if _, ok := pm.Paths[PathConfName]; !ok && PathConf.Regexp == nil {
// 			pm.createPath(PathConfName, PathConf, PathConfName, nil)
// 		}
// 	}
// }

func (pm *PathManager) doClosePath(pa *Path) {
	if pmpa, ok := pm.Paths[pa.name]; !ok || pmpa != pa {
		return
	}
	pm.removePath(pa)
}

func (pm *PathManager) doPathReady(pa *Path) {
	// if pm.hlsManager != nil {
	//     pm.hlsManager.PathReady(pa)
	// }
}

func (pm *PathManager) doPathNotReady(pa *Path) {
	// if pm.hlsManager != nil {
	//     pm.hlsManager.PathNotReady(pa)
	// }
}

func (pm *PathManager) doGetConfForPath(req PathGetConfForPathReq) {
	_, PathConf, _, err := getConfForPath(pm.PathConfs, req.name)
	if err != nil {
		req.res <- PathGetConfForPathRes{err: err}
		return
	}

	err = doAuthentication(pm.externalAuthenticationURL, pm.authMethods,
		req.name, PathConf, req.publish, req.credentials)
	if err != nil {
		req.res <- PathGetConfForPathRes{err: err}
		return
	}

	req.res <- PathGetConfForPathRes{conf: PathConf}
}

func (pm *PathManager) doDescribe(req PathDescribeReq) {
	PathConfName, PathConf, PathMatches, err := getConfForPath(pm.PathConfs, req.PathName)
	if err != nil {
		req.res <- PathDescribeRes{err: err}
		return
	}

	err = doAuthentication(pm.externalAuthenticationURL, pm.authMethods, req.PathName, PathConf, false, req.credentials)
	if err != nil {
		req.res <- PathDescribeRes{err: err}
		return
	}

	// create Path if it doesn't exist
	if _, ok := pm.Paths[req.PathName]; !ok {
		pm.createPath(PathConfName, PathConf, req.PathName, PathMatches)
	}

	req.res <- PathDescribeRes{Path: pm.Paths[req.PathName]}
}

func (pm *PathManager) doAddReader(req PathAddReaderReq) {
	PathConfName, PathConf, PathMatches, err := getConfForPath(pm.PathConfs, req.PathName)
	common.Logger.Info("PathManager.doAddReader: ", req, err)
	if err != nil {
		req.Res <- PathAddReaderRes{err: err}
		return
	}

	if !req.SkipAuth {
		err = doAuthentication(pm.externalAuthenticationURL, pm.authMethods, req.PathName, PathConf, false, req.Credentials)
		if err != nil {
			req.Res <- PathAddReaderRes{err: err}
			return
		}
	}

	// create Path if it doesn't exist
	if _, ok := pm.Paths[req.PathName]; !ok {
		pm.createPath(PathConfName, PathConf, req.PathName, PathMatches)
	}

	common.Logger.Info("PathManager.doAddReader END: ", req, PathAddReaderRes{Path: pm.Paths[req.PathName]})
	req.Res <- PathAddReaderRes{Path: pm.Paths[req.PathName]}
}

func (pm *PathManager) doAddPublisher(req PathAddPublisherReq) {
	PathConfName, PathConf, PathMatches, err := getConfForPath(pm.PathConfs, req.PathName)
	if err != nil {
		req.Res <- PathAddPublisherRes{err: err}
		return
	}

	if !req.SkipAuth {
		err = doAuthentication(pm.externalAuthenticationURL, pm.authMethods, req.PathName, PathConf, true, req.Credentials)
		if err != nil {
			req.Res <- PathAddPublisherRes{err: err}
			return
		}
	}

	// create Path if it doesn't exist
	if _, ok := pm.Paths[req.PathName]; !ok {
		pm.createPath(PathConfName, PathConf, req.PathName, PathMatches)
	}

	req.Res <- PathAddPublisherRes{Path: pm.Paths[req.PathName]}
}

func (pm *PathManager) doAPIPathsList(req PathAPIPathsListReq) {
	Paths := make(map[string]*Path)

	for name, pa := range pm.Paths {
		Paths[name] = pa
	}

	req.res <- PathAPIPathsListRes{Paths: Paths}
}

func (pm *PathManager) doAPIPathsGet(req PathAPIPathsGetReq) {
	Path, ok := pm.Paths[req.name]
	if !ok {
		req.res <- PathAPIPathsGetRes{err: common.ErrAPINotFound}
		return
	}

	req.res <- PathAPIPathsGetRes{Path: Path}
}

func (pm *PathManager) createPath(
	PathConfName string,
	PathConf *conf.PathConf,
	name string,
	matches []string,
) {
	pa := newPath(
		pm.ctx,
		pm.rtspAddress,
		pm.readTimeout,
		pm.writeTimeout,
		pm.writeQueueSize,
		pm.udpMaxPayloadSize,
		PathConfName,
		PathConf,
		name,
		matches,
		&pm.wg,
		pm.externalCmdPool,
		pm)

	pm.Paths[name] = pa

	if _, ok := pm.PathsByConf[PathConfName]; !ok {
		pm.PathsByConf[PathConfName] = make(map[*Path]struct{})
	}
	pm.PathsByConf[PathConfName][pa] = struct{}{}
}

func (pm *PathManager) removePath(pa *Path) {
	delete(pm.PathsByConf[pa.confName], pa)
	if len(pm.PathsByConf[pa.confName]) == 0 {
		delete(pm.PathsByConf, pa.confName)
	}
	delete(pm.Paths, pa.name)
}

// confReload is called by core.
// func (pm *PathManager) confReload(PathConfs map[string]*conf.PathConf) {
// 	select {
// 	case pm.chReloadConf <- PathConfs:
// 	case <-pm.ctx.Done():
// 	}
// }

// PathReady is called by Path.
func (pm *PathManager) PathReady(pa *Path) {
	select {
	case pm.chPathReady <- pa:
	case <-pm.ctx.Done():
	case <-pa.ctx.Done(): // in case PathManager is blocked by Path.wait()
	}
}

// PathNotReady is called by Path.
func (pm *PathManager) PathNotReady(pa *Path) {
	select {
	case pm.chPathNotReady <- pa:
	case <-pm.ctx.Done():
	case <-pa.ctx.Done(): // in case PathManager is blocked by Path.wait()
	}
}

// closePath is called by Path.
func (pm *PathManager) closePath(pa *Path) {
	select {
	case pm.chClosePath <- pa:
	case <-pm.ctx.Done():
	case <-pa.ctx.Done(): // in case PathManager is blocked by Path.wait()
	}
}

// getConfForPath is called by a reader or publisher.
func (pm *PathManager) getConfForPath(req PathGetConfForPathReq) PathGetConfForPathRes {
	req.res = make(chan PathGetConfForPathRes)
	select {
	case pm.chGetConfForPath <- req:
		return <-req.res

	case <-pm.ctx.Done():
		return PathGetConfForPathRes{err: fmt.Errorf("terminated")}
	}
}

// describe is called by a reader or publisher.
func (pm *PathManager) describe(req PathDescribeReq) PathDescribeRes {
	req.res = make(chan PathDescribeRes)
	select {
	case pm.chDescribe <- req:
		res1 := <-req.res
		if res1.err != nil {
			return res1
		}

		res2 := res1.Path.describe(req)
		if res2.err != nil {
			return res2
		}

		res2.Path = res1.Path
		return res2

	case <-pm.ctx.Done():
		return PathDescribeRes{err: fmt.Errorf("terminated")}
	}
}

// addPublisher is called by a publisher.
func (pm *PathManager) addPublisher(req PathAddPublisherReq) PathAddPublisherRes {
	req.Res = make(chan PathAddPublisherRes)
	select {
	case pm.chAddPublisher <- req:
		res := <-req.Res
		if res.err != nil {
			return res
		}

		return res.Path.addPublisher(req)

	case <-pm.ctx.Done():
		return PathAddPublisherRes{err: fmt.Errorf("terminated")}
	}
}

// addReader is called by a reader.
func (pm *PathManager) addReader(req PathAddReaderReq) PathAddReaderRes {
	req.Res = make(chan PathAddReaderRes)
	select {
	case pm.chAddReader <- req:
		res := <-req.Res
		if res.err != nil {
			return res
		}

		return res.Path.addReader(req)

	case <-pm.ctx.Done():
		return PathAddReaderRes{err: fmt.Errorf("terminated")}
	}
}

// apiPathsList is called by api.
func (pm *PathManager) apiPathsList() (*apiPathsList, error) {
	req := PathAPIPathsListReq{
		res: make(chan PathAPIPathsListRes),
	}

	select {
	case pm.chAPIPathsList <- req:
		res := <-req.res

		res.data = &apiPathsList{
			Items: []*apiPath{},
		}

		for _, pa := range res.Paths {
			item, err := pa.apiPathsGet(PathAPIPathsGetReq{})
			if err == nil {
				res.data.Items = append(res.data.Items, item)
			}
		}

		sort.Slice(res.data.Items, func(i, j int) bool {
			return res.data.Items[i].Name < res.data.Items[j].Name
		})

		return res.data, nil

	case <-pm.ctx.Done():
		return nil, fmt.Errorf("terminated")
	}
}

// apiPathsGet is called by api.
func (pm *PathManager) apiPathsGet(name string) (*apiPath, error) {
	req := PathAPIPathsGetReq{
		name: name,
		res:  make(chan PathAPIPathsGetRes),
	}

	select {
	case pm.chAPIPathsGet <- req:
		res := <-req.res
		if res.err != nil {
			return nil, res.err
		}

		data, err := res.Path.apiPathsGet(req)
		return data, err

	case <-pm.ctx.Done():
		return nil, fmt.Errorf("terminated")
	}
}
