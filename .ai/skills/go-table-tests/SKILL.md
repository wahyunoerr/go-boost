# Skill: Go Table-Driven Tests Pattern

Use this skill when writing or refactoring unit and integration tests in Go.

## Standard Pattern
\x60\x60\x60go
func TestService_Action(t *testing.T) {
	tests := []struct {
		name      string
		input     InputDTO
		mockSetup func(m *MockRepo)
		want      *OutputDTO
		wantErr   bool
		errTarget error
	}{
		{
			name: "success case",
			input: InputDTO{ID: 1},
			mockSetup: func(m *MockRepo) {
				m.On("FindByID", mock.Anything, uint(1)).Return(&Entity{ID: 1}, nil)
			},
			want: &OutputDTO{ID: 1},
			wantErr: false,
		},
		{
			name: "not found case",
			input: InputDTO{ID: 99},
			mockSetup: func(m *MockRepo) {
				m.On("FindByID", mock.Anything, uint(99)).Return(nil, ErrNotFound)
			},
			want: nil,
			wantErr: true,
			errTarget: ErrNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// setup mocks and execute action
		})
	}
}
\x60\x60\x60
