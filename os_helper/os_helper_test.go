package os_helper_test

import (
	"os/exec"

	. "github.com/cloudfoundry/mariadb_ctrl/os_helper"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"os"
)

var _ = Describe("OsHelper", func() {
	Describe("RunCommandWithTimeout", func() {
		var logFileName string
		h := OsHelperImpl{}

		AfterEach(func() {
			if logFileName != "" && h.FileExists(logFileName) {
				os.Remove(logFileName)
			}
		})

		It("Lets the comand run until the configured timeout", func() {
			err := h.RunCommandWithTimeout(1, "/tmp/notused", "sleep", "5")
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(Equal("Command timed out"))
		}, 2)

		It("Returns the output if it does not time out", func() {
			Expect(h.RunCommandWithTimeout(3, "/tmp/notused", "echo", "hello")).To(BeNil())
		})

		It("Writes the stdout to a log file", func() {
			logFileName := "/tmp/stdout-log"
			Expect(h.RunCommandWithTimeout(1, logFileName, "echo", "hello")).To(BeNil())
			Expect(h.FileExists(logFileName)).To(BeTrue())
			contents, err := h.ReadFile(logFileName)
			Expect(err).NotTo(HaveOccurred())
			Expect(contents).To(Equal("hello\n"))
		})

		It("Writes the stderr to a log file", func() {
			logFileName := "/tmp/stderr-log"
			Expect(h.RunCommandWithTimeout(1, logFileName, "cat", "notthere")).To(HaveOccurred())
			Expect(h.FileExists(logFileName)).To(BeTrue())
			contents, err := h.ReadFile(logFileName)
			Expect(err).NotTo(HaveOccurred())
			Expect(contents).To(Equal("cat: notthere: No such file or directory\n"))
		})
	})

	Describe("WaitForCommand", func() {

		Context("When command is bad", func() {
			It("Sends an error to a channel when the process exits", func() {
				h := OsHelperImpl{}
				cmd := exec.Command("non-existent-command")
				cmd.Start()
				ch := h.WaitForCommand(cmd)
				err := <-ch
				Expect(err).NotTo(BeNil())
			})
		})

		Context("When command is good", func() {
			It("Sends nil to a channel when the process exits", func() {
				h := OsHelperImpl{}
				cmd := exec.Command("ls")
				cmd.Start()
				ch := h.WaitForCommand(cmd)
				err := <-ch
				Expect(err).To(BeNil())
			})
		})
	})
})
