package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/oktasecuritylabs/sgt/handlers/api"
	"github.com/oktasecuritylabs/sgt/handlers/auth"
	"github.com/oktasecuritylabs/sgt/handlers/deploy"
	"github.com/oktasecuritylabs/sgt/handlers/distributed"
	"github.com/oktasecuritylabs/sgt/handlers/node"
	"github.com/oktasecuritylabs/sgt/logger"
	"github.com/urfave/negroni"
)

func server() {
	router := mux.NewRouter()
	//node endpoint
	nodeAPI := router.PathPrefix("/node").Subrouter()
	nodeAPI.Path("/configure").HandlerFunc(node.NodeConfigureRequest)
	nodeAPI.Path("/enroll").HandlerFunc(node.NodeEnrollRequest)
	//protect with uiAuth
	//Configuration (management) endpoint
	apiRouter := mux.NewRouter().PathPrefix("/api/v1/configuration").Subrouter()

	apiRouter.HandleFunc("/configs", api.GetNamedConfigs).Methods("GET", "POST")
	apiRouter.HandleFunc("/configs/{config_name}", api.ConfigurationRequest).Methods("POST")
	//Nodes
	apiRouter.HandleFunc("/nodes", api.GetNodes).Methods("GET")
	apiRouter.HandleFunc("/nodes/{node_key}", api.ConfigureNode).Methods("POST", "GET")
	apiRouter.HandleFunc("/nodes/{node_key}/approve", api.ApproveNode).Methods("POST")
	//apiRouter.HandleFunc("/nodes/approve/_bulk", api.Placeholder).Methods("POST)
	//Packs
	apiRouter.HandleFunc("/packs", api.GetQueryPacks).Methods("GET")
	apiRouter.HandleFunc("/packs/search/{search_string}", api.SearchQueryPacks).Methods("GET")
	apiRouter.HandleFunc("/packs/{pack_name}", api.ConfigurePack).Methods("POST")
	//PackQueries
	apiRouter.HandleFunc("/packqueries", api.GetPackQueries).Methods("GET")
	apiRouter.HandleFunc("/packqueries/{query_name}", api.ConfigurePackQuery)
	apiRouter.HandleFunc("/packqueries/search/{search_string}", api.SearchPackQueries)
	apiRouter.HandleFunc("/distributed/add", distributed.DistributedQueryAdd)
	//Enforce uiAuth for all our api configuration endpoints
	router.PathPrefix("/api/v1/configuration").Handler(negroni.New(
		negroni.NewRecovery(),
		negroni.HandlerFunc(auth.AnotherValidation),
		negroni.Wrap(apiRouter),
	))
	//token
	router.HandleFunc("/api/v1/get-token", auth.GetTokenHandler)
	//Distributed endpoint
	distributedRouter := mux.NewRouter().PathPrefix("/distributed").Subrouter()
	distributedRouter.HandleFunc("/read", distributed.DistributedQueryRead)
	distributedRouter.HandleFunc("/write", distributed.DistributedQueryWrite)
	//auth for distributed read/write
	router.PathPrefix("/distributed").Handler(negroni.New(
		negroni.NewRecovery(),
		negroni.HandlerFunc(auth.ValidNodeKey),
		negroni.Wrap(distributedRouter),
	))
	//Enforce auth for all our api configuration endpoints
	router.PathPrefix("/api/v1/configuration").Handler(negroni.New(
		negroni.NewRecovery(),
		negroni.HandlerFunc(auth.AnotherValidation),
		negroni.Wrap(apiRouter),
	))
	webServer := http.ListenAndServeTLS(":443",
		"fullchain.pem", "privkey.pem", handlers.LoggingHandler(os.Stdout, router))
	log.Panic("web server", webServer)
}

func main() {
	credentialsFile := flag.String("credentialsFile", "~/.aws/credentials", "path to credentials file")
	profile := flag.String("profile", "", "profile name")
	createuser := flag.Bool("create_user", false, "create new user")
	deployFlag := flag.Bool("deploy", false, "deploy new sgt environment")
	//config_file := flag.String("configfile", "", "config file for deploy")
	vpc := flag.Bool("vpc", false, "deploy VPC component")
	datastore := flag.Bool("datastore", false, "deploy datastore component")
	elasticsearch := flag.Bool("elasticsearch", false, "deploy elasticsearch component")
	firehose := flag.Bool("firehose", false, "deploy firehose component")
	s3 := flag.Bool("s3", false, "deploy s3 component")
	autoscaling := flag.Bool("autoscaling", false, "deploy autoscaling component")
	secrets := flag.Bool("secrets", false, "deploy app and node secrets")
	all := flag.Bool("all", false, "deploy all components [vpc, elasticsearch, firehose, s3, autoscaling")
	environ := flag.String("env", "", "deployment environment name")
	username := flag.String("username", "", "username")
	role := flag.String("role", "user", "user role")
	destroy := flag.Bool("destroy", false, "destroy existing infrastructure")
	newDeploy := flag.Bool("new-deployment", false, "created new deployment")
	wizard := flag.Bool("wizard", false, "Run deployment configuration wizard")
	packs := flag.Bool("packs", false, "update packs")
	configs := flag.Bool("configs", false, "update osquery configs")
	endpoints := flag.Bool("endpoints", false, "update endpoint config scripts")
	flag.Parse()
	if *wizard {
		err := deploy.DeployWizard()
		if err != nil {
			logger.Fatal(err)
		}
		return
	}
	if *newDeploy {
		envName := ""
		if len(os.Args[0]) > 0 {
			envName = os.Args[0]
			if len(envName) > 0 {
				err := deploy.CreateDeployDirectory(envName)
				if err != nil {
					logger.Error(err)
					os.Exit(1)
				}
			}

		} else {
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Enter new environment name: ")
			envName, err := reader.ReadString('\n')
			if err != nil {
				logger.Error(err)
				os.Exit(1)
			}
			if len(envName) > 0 {
				err = deploy.CreateDeployDirectory(envName)
				if err != nil {
					logger.Error(err)
					os.Exit(1)
				}
			}
		}
		logger.Warn("New deployment created.  Remember to go change the defaults in your $environment.json files!")
		return
	}
	if *createuser || *deployFlag || *destroy {
		if *createuser {
			if !(len(*username) > 4) {
				flag.Usage()
				logger.Error("username required, please pass username via -username flag")
				os.Exit(0)
			}
			if !(len(*credentialsFile) > 4) {
				flag.Usage()
				logger.Error("aws credentials file required, please pass via -credentialsFile flag")
				os.Exit(0)
			}
			if !(len(*profile) > 4) {
				flag.Usage()
				logger.Error("aws profile name required, please pass via -profile flag")
				os.Exit(0)
			}
			auth.NewUser(*credentialsFile, *profile, *username, *role)
			return
		}
		if *deployFlag {
			log.Printf("beginning deployment to %s using configuration specified in %s.json", *environ, *environ)
			log.Printf("Using credentials found in : %s", *credentialsFile)
			curdir, err := os.Getwd()
			//err := deploy.CheckEnvironMatchConfig(*environ)
			deploy.ErrorCheck(err)
			//deploy.CreateDeployDirectory(*environ)

			// Load the config to be passed to all deploy functions that require it
			config := deploy.ParseDeploymentConfig(*environ)
			if *all {
				err := deploy.DeployAll(config, curdir, *environ)
				if err != nil {
					logger.Fatal(err)
				}
			} else {
				if *vpc {
					err := deploy.VPC(curdir, *environ)
					if err != nil {
						logger.Fatal(err)
					}
				}
				if *datastore {
					err := deploy.Datastore(curdir, *environ)
					if err != nil {
						logger.Fatal(err)
					}
				}
				if *elasticsearch {
					err := deploy.Elasticsearch(curdir, *environ)
					if err != nil {
						logger.Fatal(err)
					}
				}
				if *firehose {
					err := deploy.Firehose(curdir, *environ)
					if err != nil {
						logger.Fatal(err)
					}
				}
				if *s3 {
					err := deploy.S3(curdir, *environ)
					if err != nil {
						logger.Error(err)
					}
				}
				if *secrets {
					err := deploy.Secrets(curdir, *environ)
					if err != nil {
						logger.Error(err)
					}
				}
				if *autoscaling {
					err := deploy.Autoscaling(curdir, *environ)
					if err != nil {
						logger.Error(err)
					}
				}
				if *packs {
					err := deploy.DeployDefaultPacks(config, *environ)
					if err != nil {
						logger.Error(err)
					}
				}
				if *configs {
					err := deploy.DeployDefaultConfigs(config, *environ)
					if err != nil {
						logger.Error(err)
					}
				}
				if *endpoints {
					err := deploy.GenerateEndpointDeployScripts(config, *environ)
					if err != nil {
						logger.Error(err)
					}
				}
			}
			return
		}
		if *destroy {
			curdir, err := os.Getwd()
			//err := deploy.CheckEnvironMatchConfig(*environ)
			deploy.ErrorCheck(err)
			if *all {
				err := deploy.DestroyAutoscaling(curdir, *environ)
				if err != nil {
					logger.Fatal(err)
				}
				err = deploy.DestroySecrets(curdir, *environ)
				if err != nil {
					logger.Fatal(err)
				}
				err = deploy.DestroyS3(curdir, *environ)
				if err != nil {
					logger.Fatal(err)
				}
				err = deploy.DestroyFirehose(curdir, *environ)
				if err != nil {
					logger.Fatal(err)
				}
				err = deploy.DestroyElasticsearch(curdir, *environ)
				if err != nil {
					logger.Fatal(err)
				}
				err = deploy.DestroyDatastore(curdir, *environ)
				if err != nil {
					logger.Fatal(err)
				}
				err = deploy.DestroyVPC(curdir, *environ)
				if err != nil {
					logger.Fatal(err)
				}

			} else {
				if *autoscaling {
					err := deploy.DestroyAutoscaling(curdir, *environ)
					if err != nil {
						logger.Fatal(err)
					}
				}
				if *secrets {
					err := deploy.DestroySecrets(curdir, *environ)
					if err != nil {
						logger.Fatal(err)
					}
				}
				if *s3 {
					err := deploy.DestroyS3(curdir, *environ)
					if err != nil {
						logger.Fatal(err)
					}
				}
				if *firehose {
					err := deploy.DestroyFirehose(curdir, *environ)
					if err != nil {
						logger.Fatal(err)
					}
				}
				if *elasticsearch {
					err := deploy.DestroyElasticsearch(curdir, *environ)
					if err != nil {
						logger.Fatal(err)
					}
				}
				if *datastore {
					err := deploy.DestroyDatastore(curdir, *environ)
					if err != nil {
						logger.Fatal(err)
					}
				}
				if *vpc {
					err := deploy.DestroyVPC(curdir, *environ)
					if err != nil {
						logger.Fatal(err)
					}
				}
			}
		}
	} else {
		server()
	}

}
