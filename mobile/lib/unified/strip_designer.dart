import 'dart:math' as math;
import 'package:flutter/material.dart';
import 'transport.dart';

const stripPresetNames = ['Police red / blue', 'White thunder', 'Converging red dots', 'Fade', 'Pulse'];
Json stripPreset(int index) {
  final primary = {'red': 255, 'green': 0, 'blue': 0};
  if (index == 0) return {'primitive':'alternating-zones','primary':primary,'secondary':{'red':0,'green':0,'blue':255},'period_ms':800,'step_ms':100,'swap_after_steps':4,'dim_intensity':36};
  if (index == 2) return {'primitive':'converging-points','primary':primary,'period_ms':2000,'tail_pixels':3};
  final points = index == 1 ? [[0,255],[45,24],[90,220],[145,52],[220,150],[360,0]] : index == 3 ? [[0,0],[1400,255],[2800,0]] : [[0,0],[500,255],[1000,0]];
  return {'primitive':'envelope','primary':index==1?{'red':255,'green':255,'blue':255}:{'red':0,'green':96,'blue':255},'period_ms':index==1?1600:3000,'envelope':[for(final p in points){'at_ms':p[0],'intensity':p[1]}]};
}

String? stripProgramError(Json p) {
  bool bounded(dynamic v,int low,int high)=>v is int && v>=low && v<=high;
  final period=p['period_ms'];
  if(!bounded(period,50,3600000)) return 'Cycle must be 50–3600000 ms.';
  for(final key in ['primary','secondary']) {
    if(p[key]!=null && (p[key] is! Map || ['red','green','blue'].any((c)=>!bounded(p[key][c],0,255)))) return 'Color channels must be 0–255.';
  }
  switch(p['primitive']) {
    case 'alternating-zones':
      if(!bounded(p['step_ms'],10,period)||!bounded(p['swap_after_steps'],1,1024)||p['dim_intensity']!=null&&!bounded(p['dim_intensity'],0,255)) return 'Invalid flash timing or brightness.';
    case 'converging-points':
      if(p['tail_pixels']!=null&&!bounded(p['tail_pixels'],0,100))return 'Tail must be 0–100 pixels.';
    case 'envelope':
      final points=p['envelope'];
      if(points is! List || points.length<2 || points.length>256)return 'Envelope needs 2–256 points.';
      var previous=-1;
      for(final point in points){if(point is! Map || !bounded(point['at_ms'],previous+1,period-1)||!bounded(point['intensity'],0,255))return 'Envelope times must increase inside the cycle.';previous=point['at_ms'];}
    default:return 'Choose a supported strip pattern.';
  }
  return null;
}

List<Color> stripPreview(Json p,int count,int at) {
  if(stripProgramError(p)!=null||count<1||count>100)return [];
  final period=p['period_ms'] as int,time=at%(p['period_ms'] as int);
  Color color(String key,int intensity){final c=object(p[key]);int channel(String k)=>((c[k] as int? ?? 0)*intensity~/255).clamp(0,255);return Color.fromARGB(255,channel('red'),channel('green'),channel('blue'));}
  return List.generate(count,(pixel){
    if(p['primitive']=='alternating-zones'){final step=p['step_ms'] as int;final phase=(time~/step)%math.max(2,period~/step);return color((pixel<(count+1)~/2)==(phase<(p['swap_after_steps'] as int))?'primary':'secondary',phase.isEven?255:(p['dim_intensity'] as int? ?? 0));}
    if(p['primitive']=='envelope'){final points=objects(p['envelope']);var level=0;for(var i=0;i<points.length;i++){final end=points[i]['at_ms'] as int;if(time<end){if(i>0){final previous=points[i-1],start=previous['at_ms'] as int,a=previous['intensity'] as int,b=points[i]['intensity'] as int;level=a+(b-a)*(time-start)~/(end-start);}break;}level=points[i]['intensity'] as int;}return color('primary',level);}
    final head=time*((count-1)~/2+1)~/period;
    final configured=p['tail_pixels'] as int? ?? 0,tail=math.max(3,configured==0?count~/12:configured);
    int intensity(int d)=>d<=tail?255*(tail+1-d)~/(tail+1):0;
    return color('primary',math.max(pixel<=head?intensity(head-pixel):0,pixel>=count-1-head?intensity(pixel-(count-1-head)):0));
  });
}

class StripDesigner extends StatefulWidget {
  const StripDesigner({super.key,required this.effect,required this.enabled,required this.changed});
  final Json effect; final bool enabled; final ValueChanged<Json> changed;
  @override State<StripDesigner> createState()=>_StripDesignerState();
}
class _StripDesignerState extends State<StripDesigner> {
  double phase=0;
  void program(Json p)=>widget.changed({...widget.effect,'program':p});
  Widget slider(String title,int value,int low,int high,ValueChanged<int> change)=>Column(crossAxisAlignment:CrossAxisAlignment.start,children:[Text('$title · $value'),Slider(value:value.clamp(low,high).toDouble(),min:low.toDouble(),max:high.toDouble(),onChanged:widget.enabled&&low<high?(v)=>change(v.round()):null)]);
  @override Widget build(BuildContext context){
    final p=object(widget.effect['program']),error=stripProgramError(p),count=(widget.effect['default_pixels'] as int? ?? 3).clamp(1,100);
    final points=objects(p['envelope']);
    return Column(crossAxisAlignment:CrossAxisAlignment.start,children:[
      const SizedBox(height:16),Text('Strip pattern',style:Theme.of(context).textTheme.titleMedium),
      Wrap(spacing:8,runSpacing:8,children:[for(var i=0;i<stripPresetNames.length;i++)ActionChip(label:Text(stripPresetNames[i]),onPressed:widget.enabled?()=>program(stripPreset(i)):null)]),
      if(error!=null)Padding(padding:const EdgeInsets.symmetric(vertical:12),child:Text(error,style:TextStyle(color:Theme.of(context).colorScheme.error))),
      if(error==null)...[
        const SizedBox(height:12),
        slider('Pixels',count,1,100,(v)=>widget.changed({...widget.effect,'default_pixels':v})),
        slider('FPS',widget.effect['default_fps'] as int? ?? 20,1,30,(v)=>widget.changed({...widget.effect,'default_fps':v})),
        slider('Cycle (ms)',p['period_ms'],math.max(50,math.max(points.isEmpty?0:(points.last['at_ms'] as int)+1,p['step_ms'] as int? ?? 0)),3600000,(v)=>program({...p,'period_ms':v})),
        for(final key in ['primary',if(p['primitive']=='alternating-zones')'secondary'])...[
          Text(key=='primary'?'Color':'Secondary color',style:Theme.of(context).textTheme.titleSmall),
          for(final channel in ['red','green','blue'])slider(channel,object(p[key])[channel] as int? ?? 0,0,255,(v)=>program({...p,key:{...object(p[key]),channel:v}})),
        ],
        if(p['primitive']=='alternating-zones')...[
          slider('Flash interval (ms)',p['step_ms'],10,p['period_ms'],(v)=>program({...p,'step_ms':v})),
          slider('Swap after flashes',p['swap_after_steps'],1,1024,(v)=>program({...p,'swap_after_steps':v})),
          slider('Dim brightness',p['dim_intensity'] as int? ?? 0,0,255,(v)=>program({...p,'dim_intensity':v})),
        ],
        if(p['primitive']=='converging-points')slider('Tail pixels',p['tail_pixels'] as int? ?? 0,0,100,(v)=>program({...p,'tail_pixels':v})),
        if(p['primitive']=='envelope')...[
          const Text('Brightness envelope'),
          for(var i=0;i<points.length;i++)...[
            slider('Point ${i+1} (ms)',points[i]['at_ms'],i==0?0:(points[i-1]['at_ms'] as int)+1,i+1==points.length?(p['period_ms'] as int)-1:(points[i+1]['at_ms'] as int)-1,(v)=>program({...p,'envelope':[for(var j=0;j<points.length;j++){...points[j],if(j==i)'at_ms':v}]})),
            slider('Brightness ${i+1}',points[i]['intensity'],0,255,(v)=>program({...p,'envelope':[for(var j=0;j<points.length;j++){...points[j],if(j==i)'intensity':v}]})),
          ],
        ],
        const Divider(),const Text('Preview'),Slider(value:phase,onChanged:(v)=>setState(()=>phase=v)),
        Wrap(spacing:4,runSpacing:4,children:[for(final (i,c) in stripPreview(p,count,(phase*(p['period_ms'] as int)-1).clamp(0,3600000).toInt()).indexed)Tooltip(message:'Pixel ${i+1}',child:Container(width:22,height:22,decoration:BoxDecoration(color:c,borderRadius:BorderRadius.circular(4),border:Border.all(color:Theme.of(context).dividerColor))))]),
      ],
    ]);
  }
}
