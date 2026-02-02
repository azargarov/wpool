package workerpool

import (

)


func(p *Pool[T, M])reportInternalError(e error){
	if p.OnInternaError != nil{
		p.OnInternaError(e)
	}
}

func (p *Pool[T, M])reportJobError(err error){
	if p.OnJobError != nil {
        p.OnJobError(err)
    }
}