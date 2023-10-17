package core

import (
	"context"
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"

	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/proto"
	"github.com/liuhengloveyou/livego/rpicamera"
	"github.com/liuhengloveyou/livego/stream"
	"github.com/liuhengloveyou/livego/unit"
)

func paramsFromConf(cnf *conf.Path) rpicamera.Params {
	return rpicamera.Params{
		CameraID:          cnf.RPICameraCamID,
		Width:             cnf.RPICameraWidth,
		Height:            cnf.RPICameraHeight,
		HFlip:             cnf.RPICameraHFlip,
		VFlip:             cnf.RPICameraVFlip,
		Brightness:        cnf.RPICameraBrightness,
		Contrast:          cnf.RPICameraContrast,
		Saturation:        cnf.RPICameraSaturation,
		Sharpness:         cnf.RPICameraSharpness,
		Exposure:          cnf.RPICameraExposure,
		AWB:               cnf.RPICameraAWB,
		Denoise:           cnf.RPICameraDenoise,
		Shutter:           cnf.RPICameraShutter,
		Metering:          cnf.RPICameraMetering,
		Gain:              cnf.RPICameraGain,
		EV:                cnf.RPICameraEV,
		ROI:               cnf.RPICameraROI,
		HDR:               cnf.RPICameraHDR,
		TuningFile:        cnf.RPICameraTuningFile,
		Mode:              cnf.RPICameraMode,
		FPS:               cnf.RPICameraFPS,
		IDRPeriod:         cnf.RPICameraIDRPeriod,
		Bitrate:           cnf.RPICameraBitrate,
		Profile:           cnf.RPICameraProfile,
		Level:             cnf.RPICameraLevel,
		AfMode:            cnf.RPICameraAfMode,
		AfRange:           cnf.RPICameraAfRange,
		AfSpeed:           cnf.RPICameraAfSpeed,
		LensPosition:      cnf.RPICameraLensPosition,
		AfWindow:          cnf.RPICameraAfWindow,
		TextOverlayEnable: cnf.RPICameraTextOverlayEnable,
		TextOverlay:       cnf.RPICameraTextOverlay,
	}
}

type rpiCameraSourceParent interface {
	setReady(req pathSourceStaticSetReadyReq) pathSourceStaticSetReadyRes
	setNotReady(req pathSourceStaticSetNotReadyReq)
}

type rpiCameraSource struct {
	parent rpiCameraSourceParent
}

func newRPICameraSource(
	parent rpiCameraSourceParent,
) *rpiCameraSource {
	return &rpiCameraSource{
		parent: parent,
	}
}

// run implements sourceStaticImpl.
func (s *rpiCameraSource) run(ctx context.Context, cnf *conf.Path, reloadConf chan *conf.Path) error {
	medi := &description.Media{
		Type: description.MediaTypeVideo,
		Formats: []format.Format{&format.H264{
			PayloadTyp:        96,
			PacketizationMode: 1,
		}},
	}
	medias := []*description.Media{medi}
	var stream *stream.Stream

	onData := func(dts time.Duration, au [][]byte) {
		if stream == nil {
			res := s.parent.setReady(pathSourceStaticSetReadyReq{
				desc:               &description.Session{Medias: medias},
				generateRTPPackets: true,
			})
			if res.err != nil {
				return
			}

			stream = res.stream
		}

		stream.WriteUnit(medi, medi.Formats[0], &unit.H264{
			Base: unit.Base{
				NTP: time.Now(),
				PTS: dts,
			},
			AU: au,
		})
	}

	cam, err := rpicamera.New(paramsFromConf(cnf), onData)
	if err != nil {
		return err
	}
	defer cam.Close()

	defer func() {
		if stream != nil {
			s.parent.setNotReady(pathSourceStaticSetNotReadyReq{})
		}
	}()

	for {
		select {
		case cnf := <-reloadConf:
			cam.ReloadParams(paramsFromConf(cnf))

		case <-ctx.Done():
			return nil
		}
	}
}

// apiSourceDescribe implements sourceStaticImpl.
func (*rpiCameraSource) apiSourceDescribe() proto.ApiPathSourceOrReader {
	return proto.ApiPathSourceOrReader{
		Type: "rpiCameraSource",
		ID:   "",
	}
}
