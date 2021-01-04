package webrtc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/gofrs/uuid"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v2"
	"github.com/pion/webrtc/v2/pkg/media"
)

// TODO: double check if no need TURN server here
var webrtcconfig = webrtc.Configuration{ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}}

const audioRate = 48000
const audioChannels = 2
const audioMS = 20
const audioFrame = audioRate * audioMS / 1000 * audioChannels

// InputDataPair represents input in input data channel
// type InputDataPair struct {
// 	data int
// 	time time.Time
// }

// WebRTC connection
type WebRTC struct {
	ID string

	connection  *webrtc.PeerConnection
	isConnected bool
	isClosed    bool
	// for yuvI420 image
	ImageChannel chan rtp.Packet
	AudioChannel chan []byte
	InputChannel chan []byte

	Done     bool
	lastTime time.Time
	curFPS   int
}

// OnIceCallback trigger ICE callback with candidate
type OnIceCallback func(candidate string)

// Encode encodes the input in base64
func Encode(obj interface{}) (string, error) {
	b, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(b), nil
}

// Decode decodes the input from base64
func Decode(in string, obj interface{}) error {
	b, err := base64.StdEncoding.DecodeString(in)
	if err != nil {
		return err
	}

	err = json.Unmarshal(b, obj)
	if err != nil {
		return err
	}

	return nil
}

// NewWebRTC create
func NewWebRTC() *WebRTC {
	w := &WebRTC{
		ID: uuid.Must(uuid.NewV4()).String(),

		ImageChannel: make(chan rtp.Packet, 30),// 设置的大一点
		AudioChannel: make(chan []byte, 1),
		InputChannel: make(chan []byte, 100),
	}
	return w
}

// StartClient start webrtc
func (w *WebRTC) StartClient(isMobile bool, iceCB OnIceCallback, ssrc uint32) (string, error) {
	defer func() {
		if err := recover(); err != nil {
			log.Println(err)
			w.StopClient()
		}
	}()
	var err error
	var videoTrack *webrtc.Track

	// reset client
	if w.isConnected {
		w.StopClient()
		time.Sleep(2 * time.Second)
	}

	log.Println("=== StartClient ===")
	w.connection, err = webrtc.NewPeerConnection(webrtcconfig)
	if err != nil {
		return "", err
	}

	// add video track
	videoTrack, err = w.connection.NewTrack(webrtc.DefaultPayloadTypeVP8, ssrc, "video", "app-video")

	if err != nil {
		return "", err
	}

	_, err = w.connection.AddTrack(videoTrack)
	if err != nil {
		return "", err
	}
	log.Println("Add video track")

	// add audio track
	opusTrack, err := w.connection.NewTrack(webrtc.DefaultPayloadTypeOpus, rand.Uint32(), "audio", "app-audio")
	if err != nil {
		return "", err
	}
	_, err = w.connection.AddTrack(opusTrack)
	if err != nil {
		return "", err
	}

	_, err = w.connection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RtpTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})

	// create data channel for input, and register callbacks
	// order: true, negotiated: false, id: random
	// inputTrack, err := w.connection.CreateDataChannel("app-input", nil)

	// inputTrack.OnOpen(func() {
	// 	log.Printf("Data channel '%s'-'%d' open.\n", inputTrack.Label(), inputTrack.ID())
	// })

	// Register text message handling
	// inputTrack.OnMessage(func(msg webrtc.DataChannelMessage) {
	// 	// TODO: Can add recover here
	// 	w.InputChannel <- msg.Data
	// })

	// inputTrack.OnClose(func() {
	// 	log.Println("Data channel closed")
	// 	log.Println("Closed webrtc")
	// })

	// WebRTC state callback
	w.connection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		log.Printf("ICE Connection State has changed: %s\n", connectionState.String())
		if connectionState == webrtc.ICEConnectionStateConnected {
			go func() {
				w.isConnected = true
				log.Println("ConnectionStateConnected")
				w.startStreaming(videoTrack, opusTrack)
			}()

		}
		if connectionState == webrtc.ICEConnectionStateFailed || connectionState == webrtc.ICEConnectionStateClosed || connectionState == webrtc.ICEConnectionStateDisconnected {
			w.StopClient()
		}
	})

	// 发送ice候选
	w.connection.OnICECandidate(func(iceCandidate *webrtc.ICECandidate) {
		if iceCandidate != nil {
			log.Println("OnIceCandidate:", iceCandidate.ToJSON().Candidate)
			candidate, err := Encode(iceCandidate.ToJSON())
			if err != nil {
				log.Println("Encode IceCandidate failed: " + iceCandidate.ToJSON().Candidate)
				return
			}
			iceCB(candidate)
		} else {
			// finish, send null
			iceCB("")
		}
	})

	// Stream provider supposes to send offer
	offer, err := w.connection.CreateOffer(nil)
	if err != nil {
		return "", err
	}
	log.Println("Created Offer")

	err = w.connection.SetLocalDescription(offer)
	if err != nil {
		return "", err
	}

	localSession, err := Encode(offer)
	if err != nil {
		return "", err
	}

	return localSession, nil
}

func (w *WebRTC) SetRemoteSDP(remoteSDP string) error {
	var answer webrtc.SessionDescription
	err := Decode(remoteSDP, &answer)
	if err != nil {
		log.Println("Decode remote sdp from peer failed")
		return err
	}

	fmt.Println(answer)
	fmt.Println("w: ", w)
	fmt.Println("Wconnection: ", w.connection)
	err = w.connection.SetRemoteDescription(answer)
	if err != nil {
		log.Println("Set remote description from peer failed")
		return err
	}

	log.Println("Set Remote Description")
	return nil
}

// 添加一个ice候补
func (w *WebRTC) AddCandidate(candidate string) error {
	var iceCandidate webrtc.ICECandidateInit
	err := Decode(candidate, &iceCandidate)
	if err != nil {
		log.Println("Decode Ice candidate from peer failed")
		return err
	}
	log.Println("Decoded Ice: " + iceCandidate.Candidate)

	err = w.connection.AddICECandidate(iceCandidate)
	if err != nil {
		log.Println("Add Ice candidate from peer failed")
		return err
	}

	log.Println("Add Ice Candidate: " + iceCandidate.Candidate)
	return nil
}

// StopClient disconnect
func (w *WebRTC) StopClient() {
	// if stopped, bypass
	if w.isConnected == false {
		return
	}

	log.Println("===StopClient===")
	w.isConnected = false
	if w.connection != nil {
		w.connection.Close()
	}
	w.connection = nil
	//close(w.InputChannel)
	// webrtc is producer, so we close
	// NOTE: ImageChannel is waiting for input. Close in writer is not correct for this
	close(w.ImageChannel)
	close(w.AudioChannel)
}

// IsConnected comment
func (w *WebRTC) IsConnected() bool {
	return w.isConnected
}

// 开启流，包括视频流和音频流
func (w *WebRTC) startStreaming(vp8Track *webrtc.Track, opusTrack *webrtc.Track) {
	log.Println("Start streaming")
	// receive frame buffer
	// 视频流
	go func() {
		// defer func() {
		// 	if r := recover(); r != nil {
		// 		fmt.Println("Recovered from err", r)
		// 		log.Println(debug.Stack())
		// 	}
		// }()

		for packet := range w.ImageChannel {
			// packets := vp8Track.Packetizer().Packetize(data.Data, 1)
			// for _, p := range packets {
			// 	p.Header.Timestamp = data.Timestamp

			// 往视频轨中写入packet
			err := vp8Track.WriteRTP(&packet)
			if err != nil {
				log.Println("Warn: Err write sample: ", err)
				break
			}else {
				log.Println("video stream")
			}
			// }
		}
	}()

	// send audio
	// 音频流
	go func() {
		// defer func() {
		// 	if r := recover(); r != nil {
		// 		fmt.Println("Recovered from err", r)
		// 		log.Println(debug.Stack())
		// 	}
		// }()

		for data := range w.AudioChannel {
			if !w.isConnected {
				return
			}
			err := opusTrack.WriteSample(media.Sample{
				Data:    data,
				Samples: uint32(audioFrame / audioChannels),
			})
			if err != nil {
				log.Println("Warn: Err write sample: ", err)
			}else {
				log.Println("audio stream")
			}
		}
	}()
}

// 计算FPS
func (w *WebRTC) calculateFPS() int {
	elapsedTime := time.Now().Sub(w.lastTime)
	w.lastTime = time.Now()
	curFPS := time.Second / elapsedTime
	w.curFPS = int(float32(w.curFPS)*0.9 + float32(curFPS)*0.1)
	return w.curFPS
}

// streamRTP is based on to https://github.com/pion/webrtc/tree/master/examples/rtp-to-webrtc
// It fetches from a RTP stream produced by FFMPEG and broadcast to all webRTC sessions
func (w *WebRTC) StreamRTP(offer webrtc.SessionDescription, ssrc uint32) *webrtc.Track {
	// We make our own mediaEngine so we can place the sender's codecs in it.  This because we must use the
	// dynamic media type from the sender in our answer. This is not required if we are the offerer
	mediaEngine := webrtc.MediaEngine{}
	err := mediaEngine.PopulateFromSDP(offer)
	if err != nil {
		panic(err)
	}

	// Create a video track, using the same SSRC as the incoming RTP Pack)
	// 使用相同的ssrc，标识是相同的同步信源。
	videoTrack, err := w.connection.NewTrack(webrtc.DefaultPayloadTypeVP8, ssrc, "video", "pion")
	if err != nil {
		panic(err)
	}
	if _, err = w.connection.AddTrack(videoTrack); err != nil {
		panic(err)
	}
	log.Println("video track", videoTrack)

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	// w.connection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
	// 	log.Printf("Connection State has changed %s \n", connectionState.String())
	// })

	// Set the remote SessionDescription
	// if err = conn.SetRemoteDescription(offer); err != nil {
	// 	panic(err)
	// }
	// log.Println("Done creating videotrack")

	return videoTrack
}
