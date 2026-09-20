package service

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func motionRouteIDs(c *gin.Context) (uint, uint, bool) {
	project, err1 := strconv.ParseUint(c.Param("id"), 10, 32)
	character, err2 := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err1 != nil || err2 != nil || project == 0 || character == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project or character id"})
		return 0, 0, false
	}
	return uint(project), uint(character), true
}

func (s *Service) HandleUploadCharacterMotionReference(c *gin.Context) {
	pid, cid, ok := motionRouteIDs(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, uploadLimits["video"]+uploadLimits["audio"]+uploadMultipartOverhead)
	videoFile, videoHeader, err := c.Request.FormFile("video")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "video required: " + err.Error()})
		return
	}
	defer videoFile.Close()
	video, err := readBoundedUpload(videoFile, uploadLimits["video"])
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": err.Error()})
		return
	}
	audioName := ""
	var audio []byte
	if audioFile, header, formErr := c.Request.FormFile("audio"); formErr == nil {
		defer audioFile.Close()
		audioName = header.Filename
		audio, err = readBoundedUpload(audioFile, uploadLimits["audio"])
		if err != nil {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": err.Error()})
			return
		}
	} else if !errors.Is(formErr, http.ErrMissingFile) {
		c.JSON(http.StatusBadRequest, gin.H{"error": formErr.Error()})
		return
	}
	ref, err := s.CharacterMotionReferences.Upload(pid, cid, c.PostForm("name"), videoHeader.Filename, video, audioName, audio)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, ref)
}

func (s *Service) HandleListCharacterMotionReferences(c *gin.Context) {
	pid, cid, ok := motionRouteIDs(c)
	if !ok {
		return
	}
	refs, err := s.CharacterMotionReferences.List(pid, cid)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, refs)
}
func (s *Service) HandleSelectCharacterMotionReference(c *gin.Context) {
	pid, cid, ok := motionRouteIDs(c)
	if !ok {
		return
	}
	rid, err := strconv.ParseUint(c.Param("rid"), 10, 32)
	if err != nil || rid == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reference id"})
		return
	}
	ref, err := s.CharacterMotionReferences.Select(pid, cid, uint(rid))
	if errors.Is(err, ErrCharacterMotionReferenceNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ref)
}
func (s *Service) HandleDeleteCharacterMotionReference(c *gin.Context) {
	pid, cid, ok := motionRouteIDs(c)
	if !ok {
		return
	}
	rid, err := strconv.ParseUint(c.Param("rid"), 10, 32)
	if err != nil || rid == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reference id"})
		return
	}
	err = s.CharacterMotionReferences.Delete(pid, cid, uint(rid))
	if errors.Is(err, ErrCharacterMotionReferenceNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
