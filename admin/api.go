package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/liuhengloveyou/livego/common"
	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/httpserv"
	"github.com/liuhengloveyou/livego/proto"
)

var ApiNotFound = errors.New("not found")

func interfaceIsEmpty(i interface{}) bool {
	return reflect.ValueOf(i).Kind() != reflect.Ptr || reflect.ValueOf(i).IsNil()
}

func paginate2(itemsPtr interface{}, itemsPerPage int, page int) int {
	ritems := reflect.ValueOf(itemsPtr).Elem()

	itemsLen := ritems.Len()
	if itemsLen == 0 {
		return 0
	}

	pageCount := (itemsLen / itemsPerPage)
	if (itemsLen % itemsPerPage) != 0 {
		pageCount++
	}

	min := page * itemsPerPage
	if min >= itemsLen {
		min = itemsLen - 1
	}

	max := (page + 1) * itemsPerPage
	if max >= itemsLen {
		max = itemsLen
	}

	ritems.Set(ritems.Slice(min, max))

	return pageCount
}

func paginate(itemsPtr interface{}, itemsPerPageStr string, pageStr string) (int, error) {
	itemsPerPage := 100

	if itemsPerPageStr != "" {
		tmp, err := strconv.ParseUint(itemsPerPageStr, 10, 31)
		if err != nil {
			return 0, err
		}
		itemsPerPage = int(tmp)
	}

	page := 0

	if pageStr != "" {
		tmp, err := strconv.ParseUint(pageStr, 10, 31)
		if err != nil {
			return 0, err
		}
		page = int(tmp)
	}

	return paginate2(itemsPtr, itemsPerPage, page), nil
}

func sortedKeys(paths map[string]*conf.OptionalPath) []string {
	ret := make([]string, len(paths))
	i := 0
	for name := range paths {
		ret[i] = name
		i++
	}
	sort.Strings(ret)
	return ret
}

func paramName(ctx *gin.Context) (string, bool) {
	name := ctx.Param("name")

	if len(name) < 2 || name[0] != '/' {
		return "", false
	}

	return name[1:], true
}

type Api struct {
	conf          *conf.Conf
	pathManager   proto.ApiPathManager
	rtspServer    proto.ApiRTSPServer
	rtspsServer   proto.ApiRTSPServer
	rtmpServer    proto.ApiRTMPServer
	rtmpsServer   proto.ApiRTMPServer
	webRTCManager proto.ApiWebRTCManager
	srtServer     proto.ApiSRTServer
	parent        proto.ApiParent

	httpServer *httpserv.WrappedServer
	mutex      sync.Mutex
}

func newApi(
	address string,
	readTimeout conf.StringDuration,
	conf *conf.Conf,
	pathManager proto.ApiPathManager,
	rtspServer proto.ApiRTSPServer,
	rtspsServer proto.ApiRTSPServer,
	rtmpServer proto.ApiRTMPServer,
	rtmpsServer proto.ApiRTMPServer,
	webRTCManager proto.ApiWebRTCManager,
	srtServer proto.ApiSRTServer,
	parent proto.ApiParent,
) (*Api, error) {
	a := &Api{
		conf:          conf,
		pathManager:   pathManager,
		rtspServer:    rtspServer,
		rtspsServer:   rtspsServer,
		rtmpServer:    rtmpServer,
		rtmpsServer:   rtmpsServer,
		webRTCManager: webRTCManager,
		srtServer:     srtServer,
		parent:        parent,
	}

	router := gin.New()
	router.SetTrustedProxies(nil) //nolint:errcheck

	group := router.Group("/")

	group.GET("/v3/config/global/get", a.onConfigGlobalGet)
	group.PATCH("/v3/config/global/patch", a.onConfigGlobalPatch)

	group.GET("/v3/config/pathdefaults/get", a.onConfigPathDefaultsGet)
	group.PATCH("/v3/config/pathdefaults/patch", a.onConfigPathDefaultsPatch)

	group.GET("/v3/config/paths/list", a.onConfigPathsList)
	group.GET("/v3/config/paths/get/*name", a.onConfigPathsGet)
	group.POST("/v3/config/paths/add/*name", a.onConfigPathsAdd)
	group.PATCH("/v3/config/paths/patch/*name", a.onConfigPathsPatch)
	group.POST("/v3/config/paths/replace/*name", a.onConfigPathsReplace)
	group.DELETE("/v3/config/paths/delete/*name", a.onConfigPathsDelete)

	group.GET("/v3/paths/list", a.onPathsList)
	group.GET("/v3/paths/get/*name", a.onPathsGet)

	if !interfaceIsEmpty(a.rtspServer) {
		group.GET("/v3/rtspconns/list", a.onRTSPConnsList)
		group.GET("/v3/rtspconns/get/:id", a.onRTSPConnsGet)
		group.GET("/v3/rtspsessions/list", a.onRTSPSessionsList)
		group.GET("/v3/rtspsessions/get/:id", a.onRTSPSessionsGet)
		group.POST("/v3/rtspsessions/kick/:id", a.onRTSPSessionsKick)
	}

	if !interfaceIsEmpty(a.rtspsServer) {
		group.GET("/v3/rtspsconns/list", a.onRTSPSConnsList)
		group.GET("/v3/rtspsconns/get/:id", a.onRTSPSConnsGet)
		group.GET("/v3/rtspssessions/list", a.onRTSPSSessionsList)
		group.GET("/v3/rtspssessions/get/:id", a.onRTSPSSessionsGet)
		group.POST("/v3/rtspssessions/kick/:id", a.onRTSPSSessionsKick)
	}

	if !interfaceIsEmpty(a.rtmpServer) {
		group.GET("/v3/rtmpconns/list", a.onRTMPConnsList)
		group.GET("/v3/rtmpconns/get/:id", a.onRTMPConnsGet)
		group.POST("/v3/rtmpconns/kick/:id", a.onRTMPConnsKick)
	}

	if !interfaceIsEmpty(a.rtmpsServer) {
		group.GET("/v3/rtmpsconns/list", a.onRTMPSConnsList)
		group.GET("/v3/rtmpsconns/get/:id", a.onRTMPSConnsGet)
		group.POST("/v3/rtmpsconns/kick/:id", a.onRTMPSConnsKick)
	}

	if !interfaceIsEmpty(a.webRTCManager) {
		group.GET("/v3/webrtcsessions/list", a.onWebRTCSessionsList)
		group.GET("/v3/webrtcsessions/get/:id", a.onWebRTCSessionsGet)
		group.POST("/v3/webrtcsessions/kick/:id", a.onWebRTCSessionsKick)
	}

	if !interfaceIsEmpty(a.srtServer) {
		group.GET("/v3/srtconns/list", a.onSRTConnsList)
		group.GET("/v3/srtconns/get/:id", a.onSRTConnsGet)
		group.POST("/v3/srtconns/kick/:id", a.onSRTConnsKick)
	}

	network, address := common.RestrictNetwork("tcp", address)

	var err error
	a.httpServer, err = httpserv.NewWrappedServer(
		network,
		address,
		time.Duration(readTimeout),
		"",
		"",
		router,
	)
	if err != nil {
		return nil, err
	}

	fmt.Println("listener opened on " + address)

	return a, nil
}

func (a *Api) close() {
	a.httpServer.Close()
}

// error coming from something the user inserted into the request.
func (a *Api) writeUserError(ctx *gin.Context, err error) {
	fmt.Println(err.Error())
	ctx.AbortWithStatus(http.StatusBadRequest)
}

// error coming from the server.
func (a *Api) writeServerError(ctx *gin.Context, err error) {
	fmt.Println(err.Error())
	ctx.AbortWithStatus(http.StatusInternalServerError)
}

func (a *Api) writeNotFound(ctx *gin.Context) {
	ctx.AbortWithStatus(http.StatusNotFound)
}

func (a *Api) writeServerErrorOrNotFound(ctx *gin.Context, err error) {
	if err == ApiNotFound {
		a.writeNotFound(ctx)
	} else {
		a.writeServerError(ctx, err)
	}
}

func (a *Api) onConfigGlobalGet(ctx *gin.Context) {
	a.mutex.Lock()
	c := a.conf
	a.mutex.Unlock()

	ctx.JSON(http.StatusOK, c.Global())
}

func (a *Api) onConfigGlobalPatch(ctx *gin.Context) {
	var c conf.OptionalGlobal
	err := json.NewDecoder(ctx.Request.Body).Decode(&c)
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()

	newConf := a.conf.Clone()

	newConf.PatchGlobal(&c)

	err = newConf.Check()
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	a.conf = newConf

	// since reloading the configuration can cause the shutdown of the proto.Api,
	// call it in a goroutine
	go a.parent.ApiConfigSet(newConf)

	ctx.Status(http.StatusOK)
}

func (a *Api) onConfigPathDefaultsGet(ctx *gin.Context) {
	a.mutex.Lock()
	c := a.conf
	a.mutex.Unlock()

	ctx.JSON(http.StatusOK, c.PathDefaults)
}

func (a *Api) onConfigPathDefaultsPatch(ctx *gin.Context) {
	var p conf.OptionalPath
	err := json.NewDecoder(ctx.Request.Body).Decode(&p)
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()

	newConf := a.conf.Clone()

	newConf.PatchPathDefaults(&p)

	err = newConf.Check()
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	a.conf = newConf
	a.parent.ApiConfigSet(newConf)

	ctx.Status(http.StatusOK)
}

func (a *Api) onConfigPathsList(ctx *gin.Context) {
	a.mutex.Lock()
	c := a.conf
	a.mutex.Unlock()

	data := &proto.ApiPathConfList{
		Items: make([]*conf.OptionalPath, len(c.OptionalPaths)),
	}

	for i, key := range sortedKeys(c.OptionalPaths) {
		data.Items[i] = c.OptionalPaths[key]
	}

	data.ItemCount = len(data.Items)
	pageCount, err := paginate(&data.Items, ctx.Query("itemsPerPage"), ctx.Query("page"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}
	data.PageCount = pageCount

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onConfigPathsGet(ctx *gin.Context) {
	name, ok := paramName(ctx)
	if !ok {
		a.writeUserError(ctx, fmt.Errorf("invalid name"))
		return
	}

	a.mutex.Lock()
	c := a.conf
	a.mutex.Unlock()

	p, ok := c.OptionalPaths[name]
	if !ok {
		a.writeNotFound(ctx)
		return
	}

	ctx.JSON(http.StatusOK, p)
}

func (a *Api) onConfigPathsAdd(ctx *gin.Context) { //nolint:dupl
	name, ok := paramName(ctx)
	if !ok {
		a.writeUserError(ctx, fmt.Errorf("invalid name"))
		return
	}

	var p conf.OptionalPath
	err := json.NewDecoder(ctx.Request.Body).Decode(&p)
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()

	newConf := a.conf.Clone()

	err = newConf.AddPath(name, &p)
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	err = newConf.Check()
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	a.conf = newConf
	a.parent.ApiConfigSet(newConf)

	ctx.Status(http.StatusOK)
}

func (a *Api) onConfigPathsPatch(ctx *gin.Context) { //nolint:dupl
	name, ok := paramName(ctx)
	if !ok {
		a.writeUserError(ctx, fmt.Errorf("invalid name"))
		return
	}

	var p conf.OptionalPath
	err := json.NewDecoder(ctx.Request.Body).Decode(&p)
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()

	newConf := a.conf.Clone()

	err = newConf.PatchPath(name, &p)
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	err = newConf.Check()
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	a.conf = newConf
	a.parent.ApiConfigSet(newConf)

	ctx.Status(http.StatusOK)
}

func (a *Api) onConfigPathsReplace(ctx *gin.Context) { //nolint:dupl
	name, ok := paramName(ctx)
	if !ok {
		a.writeUserError(ctx, fmt.Errorf("invalid name"))
		return
	}

	var p conf.OptionalPath
	err := json.NewDecoder(ctx.Request.Body).Decode(&p)
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()

	newConf := a.conf.Clone()

	err = newConf.ReplacePath(name, &p)
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	err = newConf.Check()
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	a.conf = newConf
	a.parent.ApiConfigSet(newConf)

	ctx.Status(http.StatusOK)
}

func (a *Api) onConfigPathsDelete(ctx *gin.Context) {
	name, ok := paramName(ctx)
	if !ok {
		a.writeUserError(ctx, fmt.Errorf("invalid name"))
		return
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()

	newConf := a.conf.Clone()

	err := newConf.RemovePath(name)
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	err = newConf.Check()
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	a.conf = newConf
	a.parent.ApiConfigSet(newConf)

	ctx.Status(http.StatusOK)
}

func (a *Api) onPathsList(ctx *gin.Context) {
	data, err := a.pathManager.ApiPathsList()
	if err != nil {
		a.writeServerError(ctx, err)
		return
	}

	data.ItemCount = len(data.Items)
	pageCount, err := paginate(&data.Items, ctx.Query("itemsPerPage"), ctx.Query("page"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}
	data.PageCount = pageCount

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onPathsGet(ctx *gin.Context) {
	name, ok := paramName(ctx)
	if !ok {
		a.writeUserError(ctx, fmt.Errorf("invalid name"))
		return
	}

	data, err := a.pathManager.ApiPathsGet(name)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onRTSPConnsList(ctx *gin.Context) {
	data, err := a.rtspServer.ApiConnsList()
	if err != nil {
		a.writeServerError(ctx, err)
		return
	}

	data.ItemCount = len(data.Items)
	pageCount, err := paginate(&data.Items, ctx.Query("itemsPerPage"), ctx.Query("page"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}
	data.PageCount = pageCount

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onRTSPConnsGet(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	data, err := a.rtspServer.ApiConnsGet(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onRTSPSessionsList(ctx *gin.Context) {
	data, err := a.rtspServer.ApiSessionsList()
	if err != nil {
		a.writeServerError(ctx, err)
		return
	}

	data.ItemCount = len(data.Items)
	pageCount, err := paginate(&data.Items, ctx.Query("itemsPerPage"), ctx.Query("page"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}
	data.PageCount = pageCount

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onRTSPSessionsGet(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	data, err := a.rtspServer.ApiSessionsGet(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onRTSPSessionsKick(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	err = a.rtspServer.ApiSessionsKick(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.Status(http.StatusOK)
}

func (a *Api) onRTSPSConnsList(ctx *gin.Context) {
	data, err := a.rtspsServer.ApiConnsList()
	if err != nil {
		a.writeServerError(ctx, err)
		return
	}

	data.ItemCount = len(data.Items)
	pageCount, err := paginate(&data.Items, ctx.Query("itemsPerPage"), ctx.Query("page"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}
	data.PageCount = pageCount

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onRTSPSConnsGet(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	data, err := a.rtspsServer.ApiConnsGet(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onRTSPSSessionsList(ctx *gin.Context) {
	data, err := a.rtspsServer.ApiSessionsList()
	if err != nil {
		a.writeServerError(ctx, err)
		return
	}

	data.ItemCount = len(data.Items)
	pageCount, err := paginate(&data.Items, ctx.Query("itemsPerPage"), ctx.Query("page"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}
	data.PageCount = pageCount

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onRTSPSSessionsGet(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	data, err := a.rtspsServer.ApiSessionsGet(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onRTSPSSessionsKick(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	err = a.rtspsServer.ApiSessionsKick(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.Status(http.StatusOK)
}

func (a *Api) onRTMPConnsList(ctx *gin.Context) {
	data, err := a.rtmpServer.ApiConnsList()
	if err != nil {
		a.writeServerError(ctx, err)
		return
	}

	data.ItemCount = len(data.Items)
	pageCount, err := paginate(&data.Items, ctx.Query("itemsPerPage"), ctx.Query("page"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}
	data.PageCount = pageCount

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onRTMPConnsGet(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	data, err := a.rtmpServer.ApiConnsGet(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onRTMPConnsKick(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	err = a.rtmpServer.ApiConnsKick(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.Status(http.StatusOK)
}

func (a *Api) onRTMPSConnsList(ctx *gin.Context) {
	data, err := a.rtmpsServer.ApiConnsList()
	if err != nil {
		a.writeServerError(ctx, err)
		return
	}

	data.ItemCount = len(data.Items)
	pageCount, err := paginate(&data.Items, ctx.Query("itemsPerPage"), ctx.Query("page"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}
	data.PageCount = pageCount

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onRTMPSConnsGet(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	data, err := a.rtmpsServer.ApiConnsGet(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onRTMPSConnsKick(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	err = a.rtmpsServer.ApiConnsKick(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.Status(http.StatusOK)
}

func (a *Api) onWebRTCSessionsList(ctx *gin.Context) {
	data, err := a.webRTCManager.ApiSessionsList()
	if err != nil {
		a.writeServerError(ctx, err)
		return
	}

	data.ItemCount = len(data.Items)
	pageCount, err := paginate(&data.Items, ctx.Query("itemsPerPage"), ctx.Query("page"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}
	data.PageCount = pageCount

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onWebRTCSessionsGet(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	data, err := a.webRTCManager.ApiSessionsGet(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onWebRTCSessionsKick(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	err = a.webRTCManager.ApiSessionsKick(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.Status(http.StatusOK)
}

func (a *Api) onSRTConnsList(ctx *gin.Context) {
	data, err := a.srtServer.ApiConnsList()
	if err != nil {
		a.writeServerError(ctx, err)
		return
	}

	data.ItemCount = len(data.Items)
	pageCount, err := paginate(&data.Items, ctx.Query("itemsPerPage"), ctx.Query("page"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}
	data.PageCount = pageCount

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onSRTConnsGet(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	data, err := a.srtServer.ApiConnsGet(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, data)
}

func (a *Api) onSRTConnsKick(ctx *gin.Context) {
	uuid, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		a.writeUserError(ctx, err)
		return
	}

	err = a.srtServer.ApiConnsKick(uuid)
	if err != nil {
		a.writeServerErrorOrNotFound(ctx, err)
		return
	}

	ctx.Status(http.StatusOK)
}

// confReload is called by core.
func (a *Api) confReload(conf *conf.Conf) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.conf = conf
}
