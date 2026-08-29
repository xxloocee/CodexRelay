package desktop

func (s *DesktopService) ClearUsage() error {
	if err := s.runtime.ClearUsage(); err != nil {
		return err
	}
	s.notifyStateChanged()
	return nil
}
