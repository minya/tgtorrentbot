package main

import ()

// func TestParseCommand(t *testing.T) {
// 	type args struct {
// 		cmdText string
// 	}
// 	tests := []struct {
// 		name    string
// 		args    args
// 		wantOk  bool
// 		wantCmd interface{}
// 	}{
// 		{
// 			name: "Should parse '/list' to ListCommand",
// 			args: args{
// 				cmdText: "/list",
// 			},
// 			wantOk:  true,
// 			wantCmd: ListCommand{},
// 		},
// 		{
// 			name: "Should parse '/list ' to ListCommand",
// 			args: args{
// 				cmdText: "/list ",
// 			},
// 			wantOk:  true,
// 			wantCmd: ListCommand{},
// 		},
// 		{
// 			name: "Should not parse 'list' to commands",
// 			args: args{
// 				cmdText: "list",
// 			},
// 			wantOk:  false,
// 			wantCmd: nil,
// 		},
// 		{
// 			name: "Should parse '/remove arg ' to RemoveCommand with id == 'arg'",
// 			args: args{
// 				cmdText: "/remove 123 ",
// 			},
// 			wantOk:  true,
// 			wantCmd: RemoveTorrentCommand{TorrentID: 123},
// 		},
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			gotOk, gotCmd := ParseCommand(tt.args.cmdText)
// 			if gotOk != tt.wantOk {
// 				t.Errorf("ParseCommand() gotOk = %v, want %v", gotOk, tt.wantOk)
// 			}
// 			if !reflect.DeepEqual(gotCmd, tt.wantCmd) {
// 				t.Errorf("ParseCommand() gotCmd = %v, want %v", gotCmd, tt.wantCmd)
// 			}
// 		})
// 	}
// }
