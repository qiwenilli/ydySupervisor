package main

import (
	"fmt"

	"html/template"

	"bufio"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/fatih/color"
	// "github.com/qiniu/log"
	//
	"github.com/urfave/cli"
	// "gopkg.in/urfave/cli.v2"
	"gopkg.in/yaml.v2"
)

func main() {

	//用于守护进程
	if err := ioutil.WriteFile("ydySupervisor.pid", []byte(fmt.Sprint(os.Getpid())), 0600); err != nil {
		color.Red("write pid error " + err.Error())
		return
	}
	if uid := os.Getuid(); uid > 0 {
		color.Red("please root run")
		return
	}

	app := cli.NewApp()
	app.Name = "ydyCron"
	app.Usage = "qiwen<34214399@qq.com>"
	app.Version = "v1.0"

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "config",
			Value: "config.yaml",
			Usage: "language for the greeting",
		},
		cli.StringFlag{
			Name:  "port",
			Value: "8412",
			Usage: "language for the greeting",
		},
	}

	var port string
	var config string

	app.Action = func(c *cli.Context) error {

		// if c.NArg() > 0 {
		//     name = c.Args().Get(0)
		// }

		port = c.String("port")
		config = c.String("config")

		return nil
	}
	app.Run(os.Args)

	//
	var web WebRequest
	web.StartWeb(port, config)
}

var lmap = new(sync.RWMutex)

type Task struct {
	Pid    int
	Lock   int
	Name   string
	User   string
	Cmd    string
	Ctime  int64
	CmdBuf *exec.Cmd
	Tail   string
}

var TaskList []*Task

type WebRequest struct{}

func (*WebRequest) StartWeb(port, config string) {

	server_list, _ := ParseConfigCmdFile(config)

	for _, s := range *server_list {

		TaskList = append(TaskList, &Task{Pid: 0, Lock: 0, Name: s.Name, User: s.User, Cmd: s.Cmd, Ctime: 0, CmdBuf: nil, Tail: ""})
	}

	// http.HandleFunc("/", defaultHandle)
	http.HandleFunc("/index", defaultHandle)
	http.HandleFunc("/run", web_run_task_handle)
	http.HandleFunc("/kill", web_kill_task_handle)
	http.HandleFunc("/killname", web_killname_task_handle)

	//
	server := &http.Server{
		Addr: ":" + port,
		// Handler:        handler,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxHeaderBytes: 1 << 10,
	}

	err := server.ListenAndServe()
	// err = server.ListenAndServeTLS("./ex/jd.crt", "./ex/jd.key")
	if err != nil {
	}
}

func defaultHandle(w http.ResponseWriter, r *http.Request) {
	//
	data := make(map[string]interface{})
	//
	_html_task_list := make(map[int]interface{}, len(TaskList))
	for i, v := range TaskList {
		var filed = make(map[string]interface{})
		//
		filed["Pid"] = v.Pid
		filed["Lock"] = v.Lock
		filed["Name"] = v.Name
		filed["Cmd"] = v.Cmd
        if v.Ctime>0{
            filed["Ctime"] = time.Unix(v.Ctime, 0).Format("2006-01-02 15:04:05")
        }else{
            filed["Ctime"] =""
        }

		//
		_html_task_list[i] = filed
	}
	//
	data["Html_task_list"] = _html_task_list

	// t, _ := template.ParseFiles("./ex/tpl/index.html")

	t := template.New("index_html_tpl.html")
	t, _ = t.Parse(index_html_tpl)

	t.Execute(w, data)
}

func web_run_task_handle(w http.ResponseWriter, r *http.Request) {

	task_id := r.FormValue("task_id")

	task_id_int, _ := strconv.Atoi(task_id)

	chan_err := make(chan string)
	defer close(chan_err)

	go execute(task_id_int, chan_err)

	err_string := <-chan_err

	fmt.Printf("%#v chan: ", err_string)

	if len(err_string) > 1 {

		io.WriteString(w, err_string)
	} else {
		io.WriteString(w, "sucess")
	}
}

func web_kill_task_handle(w http.ResponseWriter, r *http.Request) {

	task_id := r.FormValue("task_id")

	task_id_int, _ := strconv.Atoi(task_id)

	task := TaskList[task_id_int]

	output := "kill sucess"
	if task.Pid > 0 {

		for {
			if newP, err := os.FindProcess(task.Pid + 1); err != nil {
				output = fmt.Sprintf(err.Error())
			} else {
				if err := newP.Signal(syscall.SIGQUIT); err == nil {
					break
				}
			}

			if err := task.CmdBuf.Process.Kill(); err == nil {
				break
			}

			if err := task.CmdBuf.Process.Signal(syscall.SIGQUIT); err != nil {
				output = fmt.Sprintf(err.Error())
			}
		}

		task.Pid = 0
		task.Lock = 0
		task.Ctime = 0
		task.Tail = ""
	}

	io.WriteString(w, output)
}

func web_killname_task_handle(w http.ResponseWriter, r *http.Request) {

	task_id := r.FormValue("task_id")

	task_id_int, _ := strconv.Atoi(task_id)

	task := TaskList[task_id_int]

	//
	args := []string{"-c", "pkill -9 " + task.Name}
	output, err := exec.Command("/bin/bash", args...).Output()

	//
	fmt.Println(string(output), err)

	io.WriteString(w, "pkill -9 "+task.Name+" sucess")
}

func execute(task_id int, chan_err chan string) (err error) {

	output := ""

	task := TaskList[task_id]

	for {

		fmt.Println(task)

		if task.Lock == 1 {
			output = "运行中"
			break
		}

		args := []string{"-c", task.Cmd}

		task.CmdBuf = exec.Command("/bin/bash", args...)

		//
		// task.CmdBuf.Stdout = os.Stdout
		// task.CmdBuf.Stderr = os.Stderr

		stdout, err := task.CmdBuf.StdoutPipe()
		// defer stdout.Close()

		//
		err = SetUser(task)
		if err != nil {
			output = fmt.Sprintf(err.Error())
			break
		}

		//
		err = task.CmdBuf.Start()
		if err != nil {
			output = fmt.Sprintf(err.Error())
			break
		}

		lmap.Lock()

		task.Pid = task.CmdBuf.Process.Pid
		task.Ctime = time.Now().Unix()
		task.Lock = 1

		lmap.Unlock()

		rr := bufio.NewReader(stdout)
		for {
			line, err2 := rr.ReadString('\n')
			if err2 != nil || io.EOF == err2 {
				break
			}
			fmt.Println(">", line)

			task.Tail = task.Tail + fmt.Sprintf("%s", line)
		}

		//
		err = task.CmdBuf.Wait()
		if err != nil {
			output = fmt.Sprintf(err.Error())
			//
			task.Pid = 0
			task.Ctime = 0
			task.Lock = 0
			break
		}

		lmap.Lock()

		// task.Pid = task.CmdBuf.Process.Pid
		// task.Ctime = time.Now().Unix()
		task.Lock = 0

		chan_err <- ""

		lmap.Unlock()

		return nil
	}
	chan_err <- output

	lmap.Lock()
	task.Tail = output
	lmap.Unlock()

	// fmt.Println(task.CmdBuf.Stdout)
	//
	// fmt.Println(output)

	//
	return err
}

func SetUser(task *Task) (err error) {
	u, err := user.Lookup(task.User)
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return err
	}

	if task.CmdBuf.SysProcAttr == nil {
		task.CmdBuf.SysProcAttr = &syscall.SysProcAttr{}
	}
	task.CmdBuf.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}

	return nil
}

//---------------yaml
/**
* 用户权限
 */
type ConfigCmd struct {
	Name string `yaml:"name"`
	User string `yaml:"user"`
	Cmd  string `yaml:"cmd"`
}

func ParseConfigCmdData(data []byte) (*[]ConfigCmd, error) {
	var cfg []ConfigCmd
	if err := yaml.Unmarshal([]byte(data), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func ParseConfigCmdFile(fileName string) (*[]ConfigCmd, error) {
	data, err := ioutil.ReadFile(fileName)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	return ParseConfigCmdData(data)
}

//
//

var index_html_tpl = `
<!DOCTYPE html>
<html lang="zh-CN">
    <head>
        <meta http-equiv="Content-type" content="text/html; charset=utf-8">
        <title>ydyCron v1.0</title>

        <script src="http://code.jquery.com/jquery-3.1.0.min.js" integrity="sha256-cCueBR6CsyA4/9szpPfrX3s49M9vUU5BgtiJj06wt/s=" crossorigin="anonymous"></script>
        <style>
            table td {padding:5px;font-size:14px;}
            .btn_red{background-color:#ff0000; color:#ffffff;}
            .btn_green{background-color:#00ff00; }
            .btn_yellow{background-color:yellow; }
        </style>
    </head>
    <body>

<h1>ydySupervisor</h1>

<table border="0" width="100%">
    <thead>
        <tr bgcolor="#ccff99">
            <td>Pid</td>
            <td>服务</td>
            <td>启动时间</td>
            <td>命令</td>
            <td>操作</td>
        </tr>
    </thead>
    <tbody>
        {{range $k,$v := .Html_task_list}}
        <tr bgcolor="#ccff99">
            <td>{{$k}} : {{.Pid}}</td>
            <td>{{.Name}}</td>
            <td>{{.Ctime}}</td>
            <td>{{.Cmd}}</td>
            <td>
                {{if and .Pid .Lock}} 
                <font class="btn_green">[ 启动中 ]</font> 
                <button href="#" class="manual_kill_task btn_red" task_id="{{$k}}">[ 停止 ]</button>
                {{else if .Pid}} 
                <font class="btn_green">[ 已启动 ]</font> 
                <button href="#" class="manual_kill_task btn_red" task_id="{{$k}}">[ 停止 ]</button>
                {{else}} 
                <button class="manual_run_task btn_green" task_id="{{$k}}">[ 启动 ]</button> 
                <font class="btn_red">[ 已停止 ]</font>
                {{end}}

                <button class="manual_killname btn_yellow">Kill By Name</button>
            </td>
        </tr>
        {{end}}

    </tbody>
</table>


<script>
$(".manual_kill_task").click(function(){

    $.ajax( {  
        url:'/kill',// 跳转到 action  
        data:{  
            "task_id":$(this).attr("task_id")
        },  
        type:'get',  
        cache:false,  
        dataType:'html',  //xml、json、script 或 html
        success:function(data) {  
            console.log(data);
            alert(data)
        location.reload();
        },  
        error : function() {  
        }  
    });

});

$(".manual_run_task").click(function(){

    //if(confirm("确认手动运行"+ txt +" 吗？") == false){
    //    return;
    //}

    $.ajax( {  
        url:'/run',
        data:{  
            "task_id":$(this).attr("task_id")
        },  
        type:'get',  
        cache:false,  
        dataType:'html',  //xml、json、script 或 html
        success:function(data) {  
            console.log(data);
            alert(data)
        location.reload();
        },  
        error : function() {  
        }  
    });
});


$(".manual_killname").click(function(){

    //if(confirm("确认手动运行"+ txt +" 吗？") == false){
    //    return;
    //}

    $.ajax( {  
        url:'/killname',
        data:{  
            "task_id":$(this).attr("task_id")
        },  
        type:'get',  
        cache:false,  
        dataType:'html',  //xml、json、script 或 html
        success:function(data) {  
            console.log(data);
            alert(data)
        },  
        error : function() {  
        }  
    });
});

</script>
`
